[![CircleCI](https://circleci.com/gh/cosmos/gaia/tree/master.svg?style=svg)](https://circleci.com/gh/cosmos/gaia/tree/master)

# Cosmos-Hub Staking Module

This project is a demonstration of the Cosmos-Hub with basic staking
functionality staking module designed to get validators acquainted
with staking concepts and procedures.

Currently, the validator set is updated every block. The validator set is
determined as the validators with the top 100 bonded atoms. Currently, all
bonding and unbonding is instantaneous (no queue). Absent features include,
delegation, validator rewards, unbonding wait period.

## Installation
```
go get github.com/cosmos/gaia 
cd $GOPATH/src/github.com/cosmos/gaia
make all
```

See the [cosmos-sdk documentation](https://cosmos-sdk.readthedocs.io) for
information on using the gaia staking module.
