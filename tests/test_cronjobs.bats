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
@test "${BATS_TEST_FILENAME} - cronjobs - check if cronjobs are not suspended" {
    run kubectl -n ${DETIK_CLIENT_NAMESPACE} get cronjobs.batch hello -o jsonpath="{.spec.suspend}"
    debug "Command output is: $output"
    [ "$status" -eq 0 ]
    [ "$output" == "false" ]
}

# === Suspend
#
@test "${BATS_TEST_FILENAME} - onboard the testing namespace with 'controllerName' annotation" {
    run kubectl annotate --overwrite \
            ns kube-ns-suspender-testing-namespace \
            kube-ns-suspender/controllerName=kube-ns-suspender
    [ "$status" -eq 0 ]
}

# suspend the namespace
@test "${BATS_TEST_FILENAME} - action - update the testing namespace to be suspended" {
    run kubectl annotate --overwrite \
            ns kube-ns-suspender-testing-namespace \
            kube-ns-suspender/desiredState=Suspended
    [ "$status" -eq 0 ]
}

# === Post-suspend
#
@test "${BATS_TEST_FILENAME} - cronjobs - check if cronjobs are suspended" {
    run try "at most 6 times every 10s \
            to get cronjobs named 'hello' \
            and verify that '.spec.suspend' is 'true'"

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
@test "${BATS_TEST_FILENAME} - cronjobs - check if cronjobs are not suspended after resuming namespace" {
    run try "at most 6 times every 10s \
            to get cronjobs named 'hello' \
            and verify that '.spec.suspend' is 'false'"

    debug "Command output is: $output"
    [ "$status" -eq 0 ]
}

teardown() {
    [ -n "$BATS_TEST_COMPLETED" ] || touch ${BATS_PARENT_TMPNAME}.skip
}

# Note: This step seems to not be run by CI jobs on GitHub Actions
# but still usefull for local testing.
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
#   -> Same consequece: Display output only when it fails rather than always
#
# - https://bats-core.readthedocs.io/en/stable/writing-tests.html#special-variables
# 
