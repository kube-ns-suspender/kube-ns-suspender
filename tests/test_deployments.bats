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

@test "${BATS_TEST_FILENAME} - reset the debug file" {
	# This function is part of DETIK too
	reset_debug
}

# == Init
#
@test "${BATS_TEST_FILENAME} - init - test kubectl config and access" {
    run kubectl version
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - init - create '${KNS_NAMESPACE}' namespace" {
    run kubectl get ns ${KNS_NAMESPACE}
    if [[ "$status" -eq 0 ]]; then
        skip "The namespace ${KNS_NAMESPACE} already exists"
    fi

    run kubectl create ns ${KNS_NAMESPACE}
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - init - deploy kube-ns-suspender" {
    run kubectl -n ${KNS_NAMESPACE} apply -f manifests/dev/
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - init - check if kube-ns-suspender is up and running (wait max 6x10s)" {
    DETIK_CLIENT_NAMESPACE="${KNS_NAMESPACE}"

    run try "at most 6 times every 10s \
            to get pods named 'kube-ns-suspender' \
            and verify that 'status' is 'running'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - init - deploy mock manifests" {
    run kubectl apply -f manifests/testing-namespace/crds.yaml
    run kubectl apply -f manifests/testing-namespace/full.yaml
    [ "$status" -eq 0 ]
}

# == Tests resources
#
# === Pre-suspend
#
@test "${BATS_TEST_FILENAME} - deployments - check if pods 'misc-depl' are up and running (wait max 6x10s)" {
    run try "at most 6 times every 10s \
            to get pods named 'misc-depl' \
            and verify that 'status' is 'running'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - check if pods 'ignore-misc-depl' are up and running (wait max 6x10s)" {
    run try "at most 6 times every 10s \
            to get pods named 'ignore-misc-depl' \
            and verify that 'status' is 'running'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - check the number of replicas (there should be 3)" {
    run verify "there are 3 pods named 'misc-depl-*'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - check the number of ignored replicas (there should be 3)" {
    run verify "there are 3 pods named 'ignore-misc-depl-*'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

# suspend the namespace
@test "${BATS_TEST_FILENAME} - action - update the testing namespace to be suspended in the following minute" {
    run kubectl annotate --overwrite \
            ns kube-ns-suspender-testing-namespace \
            kube-ns-suspender/desiredState=Suspended
    [ "$status" -eq 0 ]
}

# === Post-suspend
#
@test "${BATS_TEST_FILENAME} - deployments - check if pods 'misc-depl-*' have 0 replicas up and running" {
    run try "at most 12 times every 10s \
            to find 0 pod named 'misc-depl' \
            with 'status' being 'running'" 
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - check if pods 'ignore-misc-depl-*' still have 3 replicas up and running" {
    run try "at most 12 times every 10s \
            to find 3 pod named 'ignore-misc-depl' \
            with 'status' being 'running'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

# unsuspend the namespace
@test "${BATS_TEST_FILENAME} - action - unsuspend the namespace" {
    run kubectl annotate --overwrite \
        ns kube-ns-suspender-testing-namespace \
        kube-ns-suspender/desiredState=Running
    [ "$status" -eq 0 ]
}

# === Post-unsuspend
#
@test "${BATS_TEST_FILENAME} - deployments - check if pods are up and running again" {
    run try "at most 12 times every 10s \
            to get pods named 'misc-depl' \
            and verify that 'status' is 'running'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - check if the number of replicas is back to original" {
    run verify "there are 3 pods named 'misc-depl'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

# Scaled to 0 test case
# === Pre-suspend
#

@test "${BATS_TEST_FILENAME} - init - scaled to 0 - scale misc-depl to 0" {
    run kubectl -n kube-ns-suspender-testing-namespace scale --replicas=0 deployment/misc-depl
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - init - scaled to 0 - scale ignore-misc-depl to 0" {
    run kubectl -n kube-ns-suspender-testing-namespace scale --replicas=0 deployment/ignore-misc-depl
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - scaled to 0 - check if pods 'misc-depl-*' have 0 replicas up and running" {
    run try "at most 12 times every 10s \
            to find 0 pod named 'misc-depl' \
            with 'status' being 'running'" 
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - scaled to 0 - check if pods 'ignore-misc-depl-*' have 0 replicas up and running" {
    run try "at most 12 times every 10s \
            to find 0 pod named 'ignore-misc-depl' \
            with 'status' being 'running'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - scaled to 0 - check the number of replicas (there should be 0)" {
    run verify "there are 0 pods named 'misc-depl-*'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - scaled to 0 - check the number of ignored replicas (there should be 0)" {
    run verify "there are 0 pods named 'ignore-misc-depl-*'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

# suspend the namespace
@test "${BATS_TEST_FILENAME} - action - scaled to 0 - update the testing namespace to be suspended in the following minute" {
    run kubectl annotate --overwrite \
            ns kube-ns-suspender-testing-namespace \
            kube-ns-suspender/desiredState=Suspended
    [ "$status" -eq 0 ]
}

# === Post-suspend
#
@test "${BATS_TEST_FILENAME} - deployments - scaled to 0 - check if pods 'misc-depl-*' have 0 replicas post-suspend" {
    run try "at most 12 times every 10s \
            to find 0 pod named 'misc-depl' \
            with 'status' being 'running'" 
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - scaled to 0 - check if pods 'ignore-misc-depl-*' have 0 replicas post-suspend" {
    run try "at most 12 times every 10s \
            to find 0 pod named 'ignore-misc-depl' \
            with 'status' being 'running'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

# unsuspend the namespace
@test "${BATS_TEST_FILENAME} - action - scaled to 0 - unsuspend the namespace" {
    run kubectl annotate --overwrite \
        ns kube-ns-suspender-testing-namespace \
        kube-ns-suspender/desiredState=Running
    [ "$status" -eq 0 ]
}

# === Post-unsuspend
#

@test "${BATS_TEST_FILENAME} - deployments - scaled to 0 - check if pods 'misc-depl-*' have 0 replicas post-unsuspend" {
    run try "at most 12 times every 10s \
            to find 0 pod named 'misc-depl' \
            with 'status' being 'running'" 
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - scaled to 0 - check if pods 'ignore-misc-depl-*' have 0 replicas post-unsuspend" {
    run try "at most 12 times every 10s \
            to find 0 pod named 'ignore-misc-depl' \
            with 'status' being 'running'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - scaled to 0 - check if the number of replicas is back to original" {
    run verify "there are 0 pods named 'misc-depl-*'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

@test "${BATS_TEST_FILENAME} - deployments - scaled to 0 - check if the number of ignored replicas is back to original" {
    run verify "there are 0 pods named 'ignore-misc-depl-*'"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

teardown() {
    [ -n "$BATS_TEST_COMPLETED" ] || touch ${BATS_PARENT_TMPNAME}.skip
}

# Note: This step seems to not be run by CI jobs on GitHub Actions
# but still useful for local testing.
teardown_file() {
    echo "----> teardown_file()"

    echo "Getting 'kube-ns-suspender' pod name"
    knsPodName=$(kubectl -n ${KNS_NAMESPACE} get pods -o name)

    echo "Getting 'kube-ns-suspender' logs"
    kubectl -n ${KNS_NAMESPACE} logs ${knsPodName#pod/} > /tmp/detik/${knsPodName#pod/}.log
}

# Notes:
#
# - https://bats-core.readthedocs.io/en/stable/faq.html#how-can-i-check-if-a-test-failed-succeeded-during-teardown
#   This could be used to collect logs only when tests failed rather than always.
#
# - https://bats-core.readthedocs.io/en/stable/faq.html#how-can-i-debug-a-failing-test
#   Use appropriate `asserts_` for your task instead of raw bash comparisons.
#   `asserts_` will print the output when the test fails while raw bash won’t.
#   -> Same consequence: Display output only when it fails rather than always
#
# - https://bats-core.readthedocs.io/en/stable/writing-tests.html#special-variables
# 
