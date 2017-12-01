#!/bin/bash

# These global variables are required for common.sh
SERVER_EXE="gaia node"
CLIENT_EXE="gaia client"
ACCOUNTS=(jae ethan bucky rigel igor)
RICH=${ACCOUNTS[0]}
DELEGATOR=${ACCOUNTS[2]}
POOR=${ACCOUNTS[4]}

BASE_DIR=$HOME/stake_test
BASE_DIR2=$HOME/stake_test2
SERVER1=$BASE_DIR/server
SERVER2=$BASE_DIR2/server

oneTimeSetUp() {
    #[ "$2" ] || echo "missing parameters, line=${LINENO}" ; exit 1;

    # These are passed in as args
    CHAIN_ID="stake_test"

    # TODO Make this more robust
    if [ "$BASE_DIR" == "$HOME/" ]; then
        echo "Must be called with argument, or it will wipe your home directory"
        exit 1
    fi

    rm -rf $BASE_DIR 2>/dev/null
    mkdir -p $BASE_DIR

    if [ "$BASE_DIR2" == "$HOME/" ]; then
        echo "Must be called with argument, or it will wipe your home directory"
        exit 1
    fi
    rm -rf $BASE_DIR2 2>/dev/null
    mkdir -p $BASE_DIR2

    # Set up client - make sure you use the proper prefix if you set
    #   a custom CLIENT_EXE
    export BC_HOME=${BASE_DIR}/client
    prepareClient

    # start the node server
    initServer $BASE_DIR $CHAIN_ID
    if [ $? != 0 ]; then return 1; fi

    initClient $CHAIN_ID
    if [ $? != 0 ]; then return 1; fi

    printf "...Testing may begin!\n\n\n"

}

oneTimeTearDown() {
    kill -9 $PID_SERVER2 >/dev/null 2>&1
    quickTearDown
}

test00GetAccount() {
    SENDER=$(getAddr $RICH)
    RECV=$(getAddr $POOR)

    assertFalse "line=${LINENO}, requires arg" "${CLIENT_EXE} query account"

    checkAccount $SENDER "9007199254740992"

    ACCT2=$(${CLIENT_EXE} query account $RECV 2>/dev/null)
    assertFalse "line=${LINENO}, has no genesis account" $?
}

test01SendTx() {
    SENDER=$(getAddr $RICH)
    RECV=$(getAddr $POOR)

    assertFalse "line=${LINENO}, missing dest" "${CLIENT_EXE} tx send --amount=992fermion --sequence=1"
    assertFalse "line=${LINENO}, bad password" "echo foo | ${CLIENT_EXE} tx send --amount=992fermion --sequence=1 --to=$RECV --name=$RICH"
    TX=$(echo qwertyuiop | ${CLIENT_EXE} tx send --amount=992fermion --sequence=1 --to=$RECV --name=$RICH)
    txSucceeded $? "$TX" "$RECV"
    HASH=$(echo $TX | jq .hash | tr -d \")
    TX_HEIGHT=$(echo $TX | jq .height)

    checkAccount $SENDER "9007199254740000" $TX_HEIGHT
    # make sure 0x prefix also works
    checkAccount "0x$SENDER" "9007199254740000" $TX_HEIGHT
    checkAccount $RECV "992" $TX_HEIGHT

    # Make sure tx is indexed
    checkSendTx $HASH $TX_HEIGHT $SENDER "992"
}

test02DeclareCandidacy() {

    # the premise of this test is to run a second validator (from rich) and then bond and unbond some tokens
    # first create a second node to run and connect to the system

    # init the second node
    SERVER_LOG2=$BASE_DIR2/node2.log
    GENKEY=$(${CLIENT_EXE} keys get ${RICH} | awk '{print $2}')
    ${SERVER_EXE} init $GENKEY --chain-id $CHAIN_ID --home=$SERVER2 >>$SERVER_LOG2
    if [ $? != 0 ]; then return 1; fi

    # copy in the genesis from the first initialization to the new server
    cp $SERVER1/genesis.json $SERVER2/genesis.json

    # point the new config to the old server location
    rm $SERVER2/config.toml
    echo 'proxy_app = "tcp://127.0.0.1:46668"
    moniker = "anonymous"
    fast_sync = true
    db_backend = "leveldb"
    log_level = "state:info,*:error"

    [rpc]
    laddr = "tcp://0.0.0.0:46667"

    [p2p]
    laddr = "tcp://0.0.0.0:46666"
    seeds = "0.0.0.0:46656"' >$SERVER2/config.toml

    # start the second node
    echo "starting second server"
    ${SERVER_EXE} start --home=$SERVER2 >>$SERVER_LOG2 2>&1 &
    sleep 5
    PID_SERVER2=$!
    disown
    if ! ps $PID_SERVER2 >/dev/null; then
        echo "**FAILED**"
        cat $SERVER_LOG2
        return 1
    fi

    # get the pubkey of the second validator

    PK2=$(cat $SERVER2/priv_validator.json | jq -r .pub_key.data)
    echo "PK2: $PK2"

    SENDER=$(getAddr $POOR)
    gaia client query account ${SENDER}

    TX=$(echo qwertyuiop | ${CLIENT_EXE} tx declare-candidacy --amount=10fermion --name=$POOR --pubkey=$PK2 --moniker=rigey)
    if [ $? != 0 ]; then return 1; fi
    HASH=$(echo $TX | jq .hash | tr -d \")
    TX_HEIGHT=$(echo $TX | jq .height)

    # better to parse data (like checkAccount) than printing out
    gaia client query account ${SENDER} --height=${TX_HEIGHT}
    gaia client query candidate --pubkey=$PK2
}

test03Delegate() {
    # send some coins to a delegator
    DELADDR=$(getAddr $DELEGATOR)
    TX=$(echo qwertyuiop | ${CLIENT_EXE} tx send --amount=15fermion --sequence=2 --to=$DELADDR --name=$RICH)
    txSucceeded $? "$TX" "$DELADDR"
    sleep 5
    echo "initial balance"
    echo "$DELADDR"
    echo "$SENDER"
    TX_HEIGHT=$(echo $TX | jq .height)
    gaia client query account ${DELADDR} 
    gaia client query account ${SENDER} 

    # delegate some coins to the new 
    echo "first delegation of 10 fermion"
    TX=$(echo qwertyuiop | ${CLIENT_EXE} tx delegate --amount=10fermion --name=$DELEGATOR --pubkey=$PK2)
    if [ $? != 0 ]; then return 1; fi
    sleep 5
    gaia client query account ${DELADDR} 
    gaia client query candidate --pubkey=$PK2 --height=${TX_HEIGHT}

    echo "second delegation of 3 fermion"
    TX_HEIGHT=$(echo $TX | jq .height)
    TX=$(echo qwertyuiop | ${CLIENT_EXE} tx delegate --amount=3fermion --name=$DELEGATOR --pubkey=$PK2)
    sleep 5
    gaia client query account ${DELADDR} 
    gaia client query candidate --pubkey=$PK2 --height=${TX_HEIGHT}
}

test04Unbond() {
    # unbond from the delegator a bit
    echo "unbond test"
    TX=$(echo qwertyuiop | ${CLIENT_EXE} tx unbond --shares=10 --name=$DELEGATOR --pubkey=$PK2)
    sleep 5
    TX_HEIGHT=$(echo $TX | jq .height)
    gaia client query account ${DELADDR} 
    gaia client query candidate --pubkey=$PK2 --height=${TX_HEIGHT}
    echo "HEREHERHRE"
    gaia client query candidates --height=${TX_HEIGHT}
    echo "sddddddddddddddddddddddd"
    gaia client query delegator-bond --delegator-address=$DELADDR --pubkey=$PK2 --height=${TX_HEIGHT}
    echo "sddddddddddddddddddddddd"
    gaia client query delegator-candidates --delegator-address=$DELADDR --height=${TX_HEIGHT}

    # unbond entirely from the delegator
    # unbond a bit from the owner
    # unbond entirely from the validator
}

# Load common then run these tests with shunit2!
CLI_DIR=$GOPATH/src/github.com/cosmos/gaia/vendor/github.com/cosmos/cosmos-sdk/tests/cli

. $CLI_DIR/common.sh
. $CLI_DIR/shunit2
