package stake

import (
	"fmt"
	"strconv"

	abci "github.com/tendermint/abci/types"

	"github.com/tendermint/tmlibs/log"

	"github.com/cosmos/cosmos-sdk"
	"github.com/cosmos/cosmos-sdk/errors"
	"github.com/cosmos/cosmos-sdk/modules/auth"
	"github.com/cosmos/cosmos-sdk/modules/coin"
	"github.com/cosmos/cosmos-sdk/stack"
	"github.com/cosmos/cosmos-sdk/state"
)

// nolint
const (
	stakingModuleName = "stake"
)

// Name is the name of the modules.
func Name() string {
	return stakingModuleName
}

// Handler - the transaction processing handler
type Handler struct {
	stack.PassInitValidate
}

// NewHandler returns a new Handler with the default Params.
func NewHandler() Handler {
	return Handler{}
}

var _ stack.Dispatchable = Handler{} // enforce interface at compile time

// Name - return stake namespace
func (Handler) Name() string {
	return stakingModuleName
}

// AssertDispatcher - placeholder for stack.Dispatchable
func (Handler) AssertDispatcher() {}

// InitState - set genesis parameters for staking
func (h Handler) InitState(l log.Logger, store state.SimpleDB,
	module, key, value string, cb sdk.InitStater) (log string, err error) {
	return "", h.initState(module, key, value, store)
}

// separated for testing
func (Handler) initState(module, key, value string, store state.SimpleDB) error {
	if module != stakingModuleName {
		return errors.ErrUnknownModule(module)
	}

	params := loadParams(store)
	switch key {
	case "allowed_bond_denom":
		params.AllowedBondDenom = value
	case "max_vals",
		"gas_bond",
		"gas_unbond":

		// TODO: enforce non-negative integers in input
		i, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("input must be integer, Error: %v", err.Error())
		}

		switch key {
		case "max_vals":
			params.MaxVals = i
		case "gas_bond":
			params.GasDelegate = uint64(i)
		case "gas_unbound":
			params.GasUnbond = uint64(i)
		}
	default:
		return errors.ErrUnknownKey(key)
	}

	saveParams(store, params)
	return nil
}

// CheckTx checks if the tx is properly structured
func (h Handler) CheckTx(ctx sdk.Context, store state.SimpleDB,
	tx sdk.Tx, _ sdk.Checker) (res sdk.CheckResult, err error) {

	err = tx.ValidateBasic()
	if err != nil {
		return res, err
	}

	// get the sender
	sender, abciRes := getTxSender(ctx)
	if abciRes.IsErr() {
		return res, abciRes
	}

	params := loadParams(store)
	// return the fee for each tx type
	switch txInner := tx.Unwrap().(type) {
	case TxDeclareCandidacy:
		return sdk.NewCheck(params.GasDeclareCandidacy, ""),
			checkTxDeclareCandidacy(txInner, sender, store)
	case TxEditCandidacy:
		return sdk.NewCheck(params.GasEditCandidacy, ""),
			checkTxEditCandidacy(txInner, sender, store)
	case TxDelegate:
		return sdk.NewCheck(params.GasDelegate, ""),
			checkTxDelegate(txInner, sender, store)
	case TxUnbond:
		return sdk.NewCheck(params.GasUnbond, ""),
			checkTxUnbond(txInner, sender, store)
	}

	return res, errors.ErrUnknownTxType(tx)
}

func checkTxDeclareCandidacy(tx TxDeclareCandidacy, sender sdk.Actor, store state.SimpleDB) error {

	// check to see if the pubkey or sender has been registered before
	candidate := loadCandidate(store, tx.PubKey)
	if candidate != nil {
		return fmt.Errorf("cannot bond to pubkey which is already declared candidacy"+
			" PubKey %v already registered with %v candidate address",
			candidate.PubKey, candidate.Owner)
	}

	return checkDenom(tx.BondUpdate, store)
}

func checkTxEditCandidacy(tx TxEditCandidacy, sender sdk.Actor, store state.SimpleDB) error {

	// candidate must already be registered
	candidate := loadCandidate(store, tx.PubKey)
	if candidate == nil { // does PubKey exist
		return fmt.Errorf("cannot delegate to non-existant PubKey %v", tx.PubKey)
	}
	return nil
}

func checkTxDelegate(tx TxDelegate, sender sdk.Actor, store state.SimpleDB) error {

	candidate := loadCandidate(store, tx.PubKey)
	if candidate == nil { // does PubKey exist
		return fmt.Errorf("cannot delegate to non-existant PubKey %v", tx.PubKey)
	}
	return checkDenom(tx.BondUpdate, store)
}

func checkTxUnbond(tx TxUnbond, sender sdk.Actor, store state.SimpleDB) error {

	//check if have enough shares to unbond
	bond := loadDelegatorBond(store, sender, tx.PubKey)
	if bond.Shares < tx.Shares {
		return fmt.Errorf("not enough bond shares to unbond, have %v, trying to unbond %v",
			bond.Shares, tx.Shares)
	}
	return nil
}

func checkDenom(tx BondUpdate, store state.SimpleDB) error {
	if tx.Bond.Denom != loadParams(store).AllowedBondDenom {
		return fmt.Errorf("Invalid coin denomination")
	}
	return nil
}

// DeliverTx executes the tx if valid
func (h Handler) DeliverTx(ctx sdk.Context, store state.SimpleDB,
	tx sdk.Tx, dispatch sdk.Deliver) (res sdk.DeliverResult, err error) {

	// TODO: remove redundancy
	// also we don't need to check the res - gas is already deducted in sdk
	_, err = h.CheckTx(ctx, store, tx, nil)
	if err != nil {
		return
	}

	sender, abciRes := getTxSender(ctx)
	if abciRes.IsErr() {
		return res, abciRes
	}

	params := loadParams(store)

	// Run the transaction
	switch _tx := tx.Unwrap().(type) {
	case TxDeclareCandidacy:
		fn := defaultTransferFn(ctx, store, dispatch)
		abciRes = runTxDeclareCandidacy(store, sender, fn, _tx)
		res.GasUsed = params.GasDeclareCandidacy
	case TxEditCandidacy:
		fn := defaultTransferFn(ctx, store, dispatch)
		abciRes = runTxEditCandidacy(store, sender, fn, _tx)
		res.GasUsed = params.GasEditCandidacy
	case TxDelegate:
		fn := defaultTransferFn(ctx, store, dispatch)
		abciRes = runTxDelegate(store, sender, fn, _tx)
		res.GasUsed = params.GasDelegate
	case TxUnbond:
		//context with hold account permissions
		params := loadParams(store)
		ctx2 := ctx.WithPermissions(params.HoldAccount)
		fn := defaultTransferFn(ctx2, store, dispatch)
		abciRes = runTxUnbond(store, sender, fn, _tx)
		res.GasUsed = params.GasUnbond
	}

	res.Data = abciRes.Data
	res.Log = abciRes.Log

	return res, nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
// These functions assume everything has been authenticated,
// now we just perform action and save

// TODO: why not just return (sdk.DeliverResult, error)?
// that is why the other interface is such, and err != nil
// is more idiomatic than res.IsErr()
func runTxDeclareCandidacy(store state.SimpleDB, sender sdk.Actor,
	transferFn transferFn, tx TxDeclareCandidacy) (res abci.Result) {

	// create and save the empty candidate
	bond := loadCandidate(store, tx.PubKey)
	if bond != nil {
		return resCandidateExistsAddr
	}
	candidate := NewCandidate(tx.PubKey, sender)
	candidate.Description = tx.Description // add the description parameters
	saveCandidate(store, candidate)

	// move coins from the sender account to a (self-bond) delegator account
	// the candidate account will be updated automatically here
	txDelegate := TxDelegate{tx.BondUpdate}
	res = runTxDelegate(store, sender, transferFn, txDelegate)
	if res.IsErr() {
		return res
	}

	return abci.OK
}

func runTxEditCandidacy(store state.SimpleDB, sender sdk.Actor,
	transferFn transferFn, tx TxEditCandidacy) (res abci.Result) {

	// Get the pubKey bond account
	candidate := loadCandidate(store, tx.PubKey)
	if candidate == nil {
		return resBondNotNominated
	}
	if candidate.Owner.Empty() { //candidate has been withdrawn
		return resBondNotNominated
	}

	//check and edit any of the editable terms
	if tx.Description.Moniker != "" {
		candidate.Description.Moniker = tx.Description.Moniker
	}
	if tx.Description.Identity != "" {
		candidate.Description.Identity = tx.Description.Identity
	}
	if tx.Description.Website != "" {
		candidate.Description.Website = tx.Description.Website
	}
	if tx.Description.Details != "" {
		candidate.Description.Details = tx.Description.Details
	}

	saveCandidate(store, candidate)

	return abci.OK
}

func runTxDelegate(store state.SimpleDB, sender sdk.Actor,
	transferFn transferFn, tx TxDelegate) (res abci.Result) {

	// Get the pubKey bond account
	candidate := loadCandidate(store, tx.PubKey)
	if candidate == nil {
		return resBondNotNominated
	}
	if candidate.Owner.Empty() { //candidate has been withdrawn
		return resBondNotNominated
	}

	// Move coins from the delegator account to the pubKey lock account
	params := loadParams(store)
	res = transferFn(sender, params.HoldAccount, coin.Coins{tx.Bond})
	if res.IsErr() {
		return res
	}

	//key := stack.PrefixedKey(coin.NameCoin, sender.Address)
	//acc := coin.Account{}
	//query.GetParsed(key, &acc, query.GetHeight(), false)
	//panic(fmt.Sprintf("debug acc: %v\n", acc))

	// Get or create the delegator bond
	bond := loadDelegatorBond(store, sender, tx.PubKey)
	if bond == nil {
		bond = &DelegatorBond{
			PubKey: tx.PubKey,
			Shares: 0,
		}
	}

	// Add shares to delegator bond and candidate
	bond.Shares += uint64(tx.Bond.Amount)
	candidate.Shares += uint64(tx.Bond.Amount)

	// Save to store
	saveCandidate(store, candidate)
	saveDelegatorBond(store, sender, bond)

	return abci.OK
}

func runTxUnbond(store state.SimpleDB, sender sdk.Actor,
	transferFn transferFn, tx TxUnbond) (res abci.Result) {

	//get delegator bond
	bond := loadDelegatorBond(store, sender, tx.PubKey)
	if bond == nil {
		return resNoDelegatorForAddress
	}

	//get pubKey candidate
	candidate := loadCandidate(store, tx.PubKey)
	if candidate == nil {
		return resNoCandidateForAddress
	}

	// subtract bond tokens from bond
	if bond.Shares < uint64(tx.Shares) {
		return resInsufficientFunds
	}
	bond.Shares -= uint64(tx.Shares)

	if bond.Shares == 0 {

		// if the bond is the owner of the candidate then
		// trigger a reject candidacy by setting Owner to Empty Actor
		if sender.Equals(candidate.Owner) {
			candidate.Owner = sdk.Actor{}
		}

		//remove the bond
		removeDelegatorBond(store, sender, tx.PubKey)
	} else {
		saveDelegatorBond(store, sender, bond)
	}

	// deduct shares from the candidate
	candidate.Shares -= uint64(tx.Shares)
	if candidate.Shares == 0 {
		removeCandidate(store, tx.PubKey)
	} else {
		saveCandidate(store, candidate)
	}

	// transfer coins back to account
	params := loadParams(store)
	returnCoins := int64(tx.Shares) //currently each share is worth one coin
	res = transferFn(params.HoldAccount, sender,
		coin.Coins{{params.AllowedBondDenom, returnCoins}})
	if res.IsErr() {
		return res
	}

	return abci.OK
}

// TODO: why don't you return a non-abci error, eg. normal error case here?

// get the sender from the ctx and ensure it matches the tx pubkey
func getTxSender(ctx sdk.Context) (sender sdk.Actor, res abci.Result) {
	senders := ctx.GetPermissions("", auth.NameSigs)
	if len(senders) != 1 {
		return sender, resMissingSignature
	}

	// TODO: ensure senders[0] matches tx.pubkey ...
	// NOTE on TODO..  right now the PubKey doesn't need to match the sender
	// and we actually don't have the means to construct the priv_validator.json
	// with its private key with current keys tooling in SDK so needs to be
	// a second key... This is still secure because you will only be able to
	// unbond to the first married account, although, you could hypotheically
	// bond some coins to somebody elses account (effectively giving them coins)
	// maybe that is worth checking more. Validators should probably be allowed
	// to use two different keys, one for validating and one with coins on it...
	// so this point may never be relevant
	return senders[0], abci.OK
}
