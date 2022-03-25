# e2e Tests scenarios


## Base

1. InitTestsEnv
    * check requirements
        + kubectl
    * Deploy `kube-ns-suspender`
        + Check it is running
    * Deploy Mock manifests (or set it in the `setup_file()` function of every tests)
        + Check resources are running as expected

2. OnBoard existing namespace
    * Pre:
        + Check Namespace does not have the annotation
        + Check `kns` metrics
    * Onboard: set `controllerName=kube-ns-suspender`
    * Post
        * Check Namespace does have the annotation
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
    * Set `nextSuspendTime=DATE+1m`
    * Wait 1m30s
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
    * Set `dalySuspendTime=DATE+1m`
    * Wait 1m30s
    * Check after (down)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended

2. Check DailySuspendTime supends resources event when user unspend manually after time
    * Check `dalySuspendTime` is set before now or wait
    * Unsuspended 
        + set `desiredState=Running`
    * Wait 1m
    * Check `desiredState=Suspended`
    * Check after (down)
        + deployment
        + statefulSet
        + CronJob-suspended
        + CronJob-unsuspended
