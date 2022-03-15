#!/usr/bin/env bats

load "lib/utils"
load "lib/detik"

DETIK_CLIENT_NAME="kubectl"
DETIK_CLIENT_NAMESPACE="kube-ns-suspender-testing-namespace"

KNS_NAMESPACE="kube-ns-suspender-testing"

diag() {
    echo "$@" | sed -e 's/^/# /' >&3 ;
}

setup() {
    [ ! -f ${BATS_PARENT_TMPNAME}.skip ] || skip "skip remaining tests"

    debug ""
	debug  ""
	debug  "-- $BATS_TEST_DESCRIPTION --"
	debug  ""
	debug  ""
}

@test "reset the debug file" {
	# This function is part of DETIK too
	reset_debug
}

@test "test kubectl config and access" {
    run kubectl version
    [ "$status" -eq 0 ]
}

@test "create '${KNS_NAMESPACE}' namespace" {
    run kubectl create ns ${KNS_NAMESPACE}
    [ "$status" -eq 0 ]
}

@test "deploy kube-ns-suspender" {
    run kubectl -n ${KNS_NAMESPACE} apply -f manifests/dev/
    [ "$status" -eq 0 ]
}

@test "check if kube-ns-suspender is up and running" {
    DETIK_CLIENT_NAMESPACE="${KNS_NAMESPACE}"

    run try "at most 6 times every 10s \
            to get pods named 'kube-ns-suspender' \
            and verify that 'status' is 'running'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "deploy mock manifests" {
    run kubectl apply -f manifests/testing-namespace/full.yaml
    [ "$status" -eq 0 ]
}

@test "check if pods 'misc-depl-*' are up and running" {
    run try "at most 6 times every 10s \
            to get pods named 'misc-depl' \
            and verify that 'status' is 'running'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "check the number of replicas" {
    run verify "there are 3 pods named 'misc-depl-*'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

# suspend the namespace
@test "update the testing namespace to be suspended in the following minute" {
    run kubectl annotate --overwrite \
            ns kube-ns-suspender-testing-namespace \
            kube-ns-suspender/dailySuspendTime=$(LC_TIME=en_US.UTF-8 date +%I:%M%p -d@"$((`date +%s`+60 ))")
    [ "$status" -eq 0 ]
}

# check the number of replicas
# it should be 0
@test "check if pods 'misc-depl-*' have 0 replicas up and running" {
    run try "at most 12 times every 10s \
            to find 0 pod named 'misc-depl' \
            with 'status' being 'running'" 
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

# unsuspend the namespace
@test "unsuspend the namespace" {
    run kubectl annotate --overwrite \
        ns kube-ns-suspender-testing-namespace \
        kube-ns-suspender/desiredState=Running
    [ "$status" -eq 0 ]
}

# check if the pods are up and running
@test "check if pods are up and running again" {
    run try "at most 12 times every 10s \
            to get pods named 'misc-depl' \
            and verify that 'status' is 'running'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

# check the number of replicas
# it should be 3
@test "check if the number of replicas is back to original" {
    run verify "there are 3 pods named 'misc-depl'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

teardown() {
    [ -n "$BATS_TEST_COMPLETED" ] || touch ${BATS_PARENT_TMPNAME}.skip
}

teardown_file() {
    echo "----> teardown_file()"

    echo "Getting 'kube-ns-suspender' pod name"
    knsPodName=$(kubectl -n ${KNS_NAMESPACE} get pods -o name)

    echo "Getting 'kube-ns-suspender' logs"
    kubectl -n ${KNS_NAMESPACE} logs ${knsPodName#pod/} > /tmp/detik/${knsPodName#pod/}.log
}
