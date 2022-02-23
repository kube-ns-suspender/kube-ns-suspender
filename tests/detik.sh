#!/usr/bin/env bats

load "lib/utils"
load "lib/detik"

DETIK_CLIENT_NAME="kubectl"
DETIK_CLIENT_NAMESPACE="kube-ns-suspender-testing-namespace"

diag() {
    echo "$@" | sed -e 's/^/# /' >&3 ;
}

setup() {
    [ ! -f ${BATS_PARENT_TMPNAME}.skip ] || skip "skip remaining tests"
}

@test "deploy kube-ns-suspender" {
    run kubectl -n kube-ns-suspender-testing apply -f manifests/dev/
    [ "$status" -eq 0 ]
}

@test "deploy mock manifests" {
    run kubectl apply -f manifests/testing-namespace/full.yaml
    [ "$status" -eq 0 ]
}

@test "check if pods are up and running" {
    run try "at most 6 times every 10s to get pods named 'misc-depl-*' and verify that 'status' is 'running'"
    [ "$status" -eq 0 ]
}

@test "check the number of replicas" {
    run verify "there are 3 pods named 'misc-depl-*'"
    [ "$status" -eq 0 ]
}

# suspend the namespace
@test "update the testing namespace to be suspended in the following minute" {
    run kubectl annotate --overwrite ns kube-ns-suspender-testing-namespace kube-ns-suspender/dailySuspendTime=$(LC_TIME=en_US.UTF-8 date -d '+1 minute' +"%I:%M%p")
    [ "$status" -eq 0 ]
}

# check the number of replicas
# it should be 0
@test "wait for the namespace to be suspended" {
    run try "at most 12 times every 8s to find 0 pod named 'misc-depl-*' with 'status' being 'running'" 
    [ "$status" -eq 0 ]
}

# unsuspend the namespace
@test "unsuspend the namespace" {
    run kubectl annotate --overwrite ns kube-ns-suspender-testing-namespace kube-ns-suspender/desiredState=Running
    [ "$status" -eq 0 ]
}

# check if the pods are up and running
@test "check if pods are up and running again" {
    run try "at most 6 times every 8s to get pods named 'misc-depl-*' and verify that 'status' is 'running'"
    [ "$status" -eq 0 ]
}

# check the number of replicas
# it should be 3
@test "check if the number of replicas is back to original" {
    run verify "there are 3 pods named 'misc-depl-*'"
    [ "$status" -eq 0 ]
}

teardown() {
    [ -n "$BATS_TEST_COMPLETED" ] || touch ${BATS_PARENT_TMPNAME}.skip
}
