package commands

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/cosmos/gaia/modules/stake"

	"github.com/cosmos/cosmos-sdk/client/commands"
	"github.com/cosmos/cosmos-sdk/client/commands/query"
	"github.com/cosmos/cosmos-sdk/stack"
)

//nolint
var (
	CmdQueryValidators = &cobra.Command{
		Use:   "validators",
		Short: "Query for the validator set",
		RunE:  cmdQueryValidators,
	}
	// TODO individual validators
	//CmdQueryValidator = &cobra.Command{
	//Use:   "validator",
	//Short: "Query a validator account",
	//RunE:  cmdQueryValidator,
	//}
)

func cmdQueryValidators(cmd *cobra.Command, args []string) error {
	var bonds stake.ValidatorBonds

	prove := !viper.GetBool(commands.FlagTrustNode)
	key := stack.PrefixedKey(stake.Name(), stake.BondKey)
	h, err := query.GetParsed(key, &bonds, query.GetHeight(), prove)
	if err != nil {
		return err
	}

	return query.OutputProof(bonds, h)
}
