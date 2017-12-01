package stake

import (
	"bytes"
	"sort"

	"github.com/cosmos/cosmos-sdk"
	"github.com/cosmos/cosmos-sdk/modules/coin"
	"github.com/cosmos/cosmos-sdk/state"

	abci "github.com/tendermint/abci/types"
	crypto "github.com/tendermint/go-crypto"
	wire "github.com/tendermint/go-wire"
	cmn "github.com/tendermint/tmlibs/common"
)

// Params defines the high level settings for staking
type Params struct {
	HoldAccount sdk.Actor `json:"hold_account"` // PubKey where all bonded coins are held

	MaxVals          int    `json:"max_vals"`           // maximum number of validators
	AllowedBondDenom string `json:"allowed_bond_denom"` // bondable coin denomination

	// gas costs for txs
	GasDeclareCandidacy uint64 `json:"gas_declare_candidacy"`
	GasEditCandidacy    uint64 `json:"gas_edit_candidacy"`
	GasDelegate         uint64 `json:"gas_delegate"`
	GasUnbond           uint64 `json:"gas_unbond"`
}

func defaultParams() Params {
	return Params{
		HoldAccount:         sdk.NewActor(stakingModuleName, []byte("77777777777777777777777777777777")),
		MaxVals:             100,
		AllowedBondDenom:    "fermion",
		GasDeclareCandidacy: 20,
		GasEditCandidacy:    20,
		GasDelegate:         20,
		GasUnbond:           20,
	}
}

//--------------------------------------------------------------------------------

// Candidate defines the total amount of bond shares and their exchange rate to
// coins, associated with a single validator. Accumulation of interest is modelled
// as an in increase in the exchange rate, and slashing as a decrease.
// When coins are delegated to this validator, the validator is credited
// with a DelegatorBond whose number of bond shares is based on the amount of coins
// delegated divided by the current exchange rate. Voting power can be calculated as
// total bonds multiplied by exchange rate.
// NOTE if the Owner.Empty() == true then this is a revoked candidate
type Candidate struct {
	PubKey      crypto.PubKey `json:"pub_key"`      // Pubkey of candidate
	Owner       sdk.Actor     `json:"owner"`        // Sender of BondTx - UnbondTx returns here
	Shares      uint64        `json:"shares"`       // Total number of delegated shares to this candidate, equivalent to coins held in bond account
	VotingPower uint64        `json:"voting_power"` // Voting power if pubKey is a considered a validator
	Description Description   `json:"description"`  // Description terms for the candidate
}

// Description - description fields for a candidate
type Description struct {
	Moniker  string `json:"moniker"`
	Identity string `json:"identity"`
	Website  string `json:"website"`
	Details  string `json:"details"`
}

// NewCandidate - initialize a new candidate
func NewCandidate(pubKey crypto.PubKey, owner sdk.Actor) *Candidate {
	return &Candidate{
		PubKey:      pubKey,
		Owner:       owner,
		Shares:      0,
		VotingPower: 0,
	}
}

// ABCIValidator - Get the validator from a bond value
func (c Candidate) ABCIValidator() *abci.Validator {
	return &abci.Validator{
		PubKey: wire.BinaryBytes(c.PubKey),
		Power:  c.VotingPower,
	}
}

//--------------------------------------------------------------------------------

// TODO replace with sorted multistore functionality

// Candidates - list of Candidates
type Candidates []*Candidate

var _ sort.Interface = Candidates{} //enforce the sort interface at compile time

// nolint - sort interface functions
func (cs Candidates) Len() int      { return len(cs) }
func (cs Candidates) Swap(i, j int) { cs[i], cs[j] = cs[j], cs[i] }
func (cs Candidates) Less(i, j int) bool {
	vp1, vp2 := cs[i].VotingPower, cs[j].VotingPower
	d1, d2 := cs[i].Owner, cs[j].Owner

	//note that all ChainId and App must be the same for a group of candidates
	if vp1 != vp2 {
		return vp1 > vp2
	}
	return bytes.Compare(d1.Address, d2.Address) == -1
}

// Sort - Sort the array of bonded values
func (cs Candidates) Sort() {
	sort.Sort(cs)
}

// UpdateVotingPower - voting power based on bond tokens and exchange rate
// TODO: make not a function of ValidatorBonds as validatorbonds can be loaded from the store
func (cs Candidates) UpdateVotingPower(store state.SimpleDB) (changed bool) {
	for _, c := range cs {
		if c.VotingPower != c.Shares {
			changed = true
			c.VotingPower = c.Shares
		}
	}

	// we don't write anything if nothing changes
	if !changed {
		return false
	}

	// Now sort and truncate the power
	cs.Sort()
	for i, c := range cs {
		if i >= loadParams(store).MaxVals {
			c.VotingPower = 0
		}
		saveCandidate(store, c)
	}
	return true
}

// GetValidators - get the most recent updated validator set from the
// Candidates. These bonds are already sorted by VotingPower from
// the UpdateVotingPower function which is the only function which
// is to modify the VotingPower
func (cs Candidates) GetValidators(store state.SimpleDB) []Candidate {

	//test if empty
	if len(cs) == 1 {
		if cs[0].VotingPower == 0 {
			return nil
		}
	}

	maxVals := loadParams(store).MaxVals
	validators := make([]Candidate, cmn.MinInt(len(cs), maxVals))
	for i, c := range cs {
		if c.VotingPower == 0 { //exit as soon as the first Voting power set to zero is found
			break
		}
		if i >= maxVals {
			return validators
		}
		validators[i] = *c
	}

	return validators
}

// ValidatorsDiff - get the difference in the validator set from the input validator set
func ValidatorsDiff(previous, current []Candidate, store state.SimpleDB) (diff []*abci.Validator) {

	//TODO: do something more efficient possibly by sorting first

	// calculate any differences from the previous to the new validator set
	// first loop through the previous validator set, and then catch any
	// missed records in the new validator set
	diff = make([]*abci.Validator, 0, loadParams(store).MaxVals)

	for _, prevVal := range previous {
		abciVal := prevVal.ABCIValidator()
		found := false
		candidate := loadCandidate(store, prevVal.PubKey)
		if candidate != nil {
			found = true
			if candidate.VotingPower != prevVal.VotingPower {
				diff = append(diff, &abci.Validator{abciVal.PubKey, candidate.VotingPower})
			}
		}

		if !found {
			diff = append(diff, &abci.Validator{abciVal.PubKey, 0})
		}
	}

	// TODO should use "notfound" variable which starts with the "current" set and is reduced
	//  to the notfound set in the above loop. Then simply loop through this. Really only one loop
	//  as the above loop.

	for _, curVal := range current {

		//loop through diff to see if there where any missed
		found := false
		for _, prevVal := range previous {
			if prevVal.PubKey.Empty() {
				continue
			}
			if prevVal.PubKey.Equals(curVal.PubKey) {
				found = true
				break
			}
		}

		if !found {
			diff = append(diff, curVal.ABCIValidator())
		}

	}

	return
}

//--------------------------------------------------------------------------------

// DelegatorBond represents some bond tokens held by an account.
// It is owned by one delegator, and is associated with the voting power of one pubKey.
type DelegatorBond struct {
	PubKey crypto.PubKey
	Shares uint64
}

//--------------------------------------------------------------------------------

// transfer coins
type transferFn func(from sdk.Actor, to sdk.Actor, coins coin.Coins) abci.Result

// default transfer runs full DeliverTX
func defaultTransferFn(ctx sdk.Context, store state.SimpleDB, dispatch sdk.Deliver) transferFn {
	return func(sender, receiver sdk.Actor, coins coin.Coins) (res abci.Result) {
		// Move coins from the delegator account to the pubKey lock account
		send := coin.NewSendOneTx(sender, receiver, coins)

		// If the deduction fails (too high), abort the command
		_, err := dispatch.DeliverTx(ctx, store, send)
		if err != nil {
			return abci.ErrInsufficientFunds.AppendLog(err.Error())
		}
		return
	}
}
