# kube-ns-suspender

Inspired by https://codeberg.org/hjacobs/kube-downscaler/src/branch/main

## Goals

* @govirtuo, an FeatureEnvironment === a whole `namespace` with many `Deployments` and `StatefulSets`, `CronJobs` in there.
* We want to "pause" an FE by downscaling every "deployments" (:point_up:) to 0 for resources and costs motivations.


### Behaviors

### Pausing (Auto)

* A user creates an FE one day at a certain time
* "deployments" are "up" and isolated at the Namespace level
* Everyday, at a specific time (configurable), the controllerchecks:
    - Is the FEs "paused"
    - if NOT, "pauses" it
        + Iterate around all "deployments" in the Namespace
        + Set the `replicas` field to 0

### Pausing (ByUser)

* A user creates an FE one day at a certain time
* "deployments" are "up" and isolated at the Namespace level
* The user pus an `annotation` on the Namespace
    - The controller detects it and "pause" the FE
        + Iterate around all "deployments" in the Namespace
        + Set the `replicas` field to 0

### UnPausing (ByUser)

* if a user needs to work on its FE:
* Using a tool, an annotation is put / removed on the namespace representing the FE
* The controller watch this annotation
* And restore the `replicas` value



## Specs

### Annotations

2 annotations on each `Namespace`:
* `kube-fesuspender/desiredState`: {Running, Suspended}
* `kube-fesuspender/suspendAt`  (Optional)

On downstream Objects (technical annotation)
* `kube-fesuspender/originalReplicas`


### Behavior

`kube-fescaler/desiredState`:
  * Suspended => check if replica set is 0, and act accordingly (ignore or scale down)
  * Running => check if replica set is > 0

Check if state matches the number of replicas (i.e if state == Nominal && replicas == 0, there is an issue). State should be the reference:
* if state == Nominal && replicas == 0, increase the number of replicas if every deployment
* if state == DownScaled && replicas == n + 1, downscale the deployments
This allows us to edit only the state annotation when we need to scale up the deployments.

`kube-fescaler/suspendAt (Optional)`:
time to downscale the number of replicas in the namespace
  -> define a time format (iso8601 ?)



## Questions

- [x] How to keep the initial number of replicas in memory ? Persistent/volatile ? Redis ?
    + When a namespace is added to watch list, add `kube-fescaler/originalReplicas` annotation on each downstream object (deployments...).

- [x] How to manually scale up the deployment 
    + Edit the live manifest of the Namespace by updating the `kube-fesuspender/desiredState` annotation to "Running"

- [x] What about crons ? how to stop them ?
    + `CronJobs` have a `suspended` field. We need to handle these downstream Objects and update this field.

