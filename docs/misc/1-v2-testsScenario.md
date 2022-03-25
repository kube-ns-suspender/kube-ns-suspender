# e2e Tests scenarios


## Base

1. InitTestsEnv
    * check requirements
        + kubectl
    * Deploy `kube-ns-suspender`
    * Deploy Mock manifests (or set it in the `setup_file()` function of every tests)

2. OnBoard existing namespace
    * Check Namespace before
    * Onboard
    * Check Namespace after
    * Check `kns` metrics

// Always, collect:
// * kns logs
// * Namespace definition


## Feature: Manual suspend/unsuspend

1. ManualSuspension of new NS
    * Check before (up)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended
    * set `desiredState=Suspended`
    * Check after (down)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended

2. ManualUnsuspension
    * Check before (down)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended
    * set `desiredState=Running`
    * Check before (up)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended

3. ManualSuspension of existing NS (It already has some annotations)
    * Check before (up)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended
    * set `desiredState=Suspended`
    * Check before (down)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended

// Always, collect:
// * kns logs
// * Namespace definition

## Fearture: nextSuspendTime

1. Check Auto NextSuspendTime
    * Check before (down)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended
    * set `desiredState=Running`
    * Check after (up)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended
    * Check if `nextSuspendTime` is set

2. Check Manual NextSuspendTime
    * Unsuspended if needed
        + set `desiredState=Running`
    * Check before (up)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended
    * Set `nextSuspendTime=DATE+10s`
    * Wait 30s
    * Check after (down)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended


## Fearture: dailySuspendTime

1. Check DailySuspendTime
    * Unsuspended if needed
        + set `desiredState=Running`
    * Check before (up)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended
    * Set `dalySuspendTime=DATE+10s`
    * Wait 30s
    * Check after (down)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended

1. CheckNsisSuspended