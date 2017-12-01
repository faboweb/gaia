package stake

import (
	"github.com/cosmos/cosmos-sdk/state"
	abci "github.com/tendermint/abci/types"
)

// Tick - Called every block even if no transaction,
// process all queues, validator rewards, and calculate the validator set difference
func Tick(store state.SimpleDB) (diffVal []*abci.Validator, err error) {
	// Determine the validator set changes
	candidates := loadCandidates(store)
	startVal := candidates.GetValidators(store)
	changed := candidates.UpdateVotingPower(store)
	if !changed {
		return
	}
	newVal := candidates.GetValidators(store)
	diffVal = ValidatorsDiff(startVal, newVal, store)
	return
}
