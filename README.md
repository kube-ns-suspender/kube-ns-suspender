# kube-ns-suspender

Kubernetes controller managing namespaces life cycle.

- [kube-ns-suspender](#kube-ns-suspender)
  - [Goal](#goal)
  - [Usage](#usage)
    - [Internals](#internals)
    - [Flags](#flags)
    - [Resources](#resources)
    - [States](#states)
    - [Annotations](#annotations)
      - [On namespaces](#on-namespaces)
      - [On resources](#on-resources)
        - [Deployments and Stateful Sets](#deployments-and-stateful-sets)
        - [Cronjobs](#cronjobs)
  - [Contributing](#contributing)
  - [License](#license)

## Goal

This controller watches the cluster's namespaces and "suspends" them by scaling to 0 some of the resources within those namespaces at a given time.
However, once a namespace is in a "suspended" state, it will not be restarted automatically the following day (or whatever). This allows to "reactivate" namespaces only when required, and reduce costs.

## Usage

### Internals

/* do a pretty scheme about the internals */

### Flags

/* explain the different flags, the associated env vars... */

### Resources

Currently supported resources are:

* deployments
* stateful sets
* cronjobs

### States

Namespaces watched by `kube-ns-suspender` can be in 3 differents states:

* Running: the namespace is "up", and all the resources have the desired number of replicas.
* Suspended: the namespace is "paused", and all the supported resources are scaled down to 0 or suspended.
* Running Forced: the namespace has been suspended, and then reactivated manually. It will be "running" for a pre-defined duration then will go back to the "suspended" state.

### Annotations

#### On namespaces

In order for a namespace to be watched by the controller, it needs to have the `kube-ns-suspender/desiredState` annotation set to any of the supported values, which are:

* `Running`
* `RunningForced`
* `Suspended`

To be suspended at a given time, a namespace must have the annotation `kube-ns-suspender/suspendAt` set to a valid value.
Valid values are any values that match the [`time.Kitchen`](https://pkg.go.dev/time#pkg-constants) time format, for example: `8:15PM`, `12:45AM`...

#### On resources

##### Deployments and Stateful Sets

##### Cronjobs

## Contributing

/* add CONTRIBUTING file at root */

## License

[MIT](https://choosealicense.com/licenses/mit/)