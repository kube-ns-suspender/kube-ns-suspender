# kube-ns-suspender

Kubernetes controller managing namespaces life cycle.

- [kube-ns-suspender](#kube-ns-suspender)
  - [Goal](#goal)
  - [Usage](#usage)
    - [Internals](#internals)
      - [The watcher](#the-watcher)
      - [The suspender](#the-suspender)
    - [Flags](#flags)
    - [Resources](#resources)
    - [States](#states)
    - [Annotations](#annotations)
      - [On namespaces](#on-namespaces)
      - [On resources](#on-resources)
        - [Deployments and Stateful Sets](#deployments-and-stateful-sets)
        - [Cronjobs](#cronjobs)
  - [Development flow](#development-flow)
  - [Testing](#testing)
  - [Contributing](#contributing)
  - [License](#license)

## Goal

This controller watches the cluster's namespaces and "suspends" them by scaling to 0 some of the resources within those namespaces at a given time.
However, once a namespace is in a "suspended" state, it will not be restarted automatically the following day (or whatever). This allows to "reactivate" namespaces only when required, and reduce costs.

## Usage

### Internals

This controller can be splitted into 2 parts:

* The watcher
* The suspender

#### The watcher

The watcher function is charged to check every X seconds (X being set by the flag `-watcher-idle` or by the `KUBE_NS_SUSPENDER_WATCHER_IDLE` environement variable) all the namespaces. When it found namespace that have the `kube-ns-suspender/desiredState` annotation, it sends it to the suspender. It also manages all the metrics that are exposed about the watched namespaces states.

#### The suspender

The suspender function does all the work of reading namespaces/resources annotations, and (un)suspending them when required.

### Flags

/* explain the different flags, the associated env vars... */

### Resources

Currently supported resources are:

* [deployments](#deployments-and-stateful-sets)
* [stateful sets](#deployments-and-stateful-sets)
* [cronjobs](#cronjobs)

### States

Namespaces watched by `kube-ns-suspender` can be in 3 differents states:

* Running: the namespace is "up", and all the resources have the desired number of replicas.
* Suspended: the namespace is "paused", and all the supported resources are scaled down to 0 or suspended.

### Annotations

Annotations are employed to save the original state of a resource.

#### On namespaces

In order for a namespace to be watched by the controller, it needs to have the `kube-ns-suspender/desiredState` annotation set to any of the supported values, which are:

* `Running`
* `Suspended`

To be suspended at a given time, a namespace must have the annotation `kube-ns-suspender/suspendAt` set to a valid value.
Valid values are any values that match the [`time.Kitchen`](https://pkg.go.dev/time#pkg-constants) time format, for example: `8:15PM`, `12:45AM`...

#### On resources

##### Deployments and Stateful Sets

As those resources have a `spec.replicas` value, they must have a `kube-ns-suspender/originalReplicas` annotation that must be the same as the `spec.replicas` value. This annotation will be used when a resource will be "unsuspended" to set the original number of replicas.

##### Cronjobs

Cronjobs have a `spec.suspend` value that indicates if they must be runned or not. As this value is a boolean, **no other annotations are required**.

## Development flow

To test the modifications in real-time, this project uses [`devspace`](https://devspace.sh/). It is configured to use the manifests in `manifests/dev/`.

First, set the namespace to use:

```
devspace use namespace kube-ns-suspender-testing
```

Deploy the testing namespace with mock resources:

```
kubectl apply -f manifests/testing-namespace
```

Then deploy your dev version of `kube-ns-suspender`:

```
devspace dev
```

> :warning:
> 
> `devspace` and `kubectl` will deploy the manifests in the cluster set by the current context. be sure to **not** deploy in the wrong cluster.
>

Once the dev deployment is over, you'll have access to a shell in the container. You can start developing!

When you are done, you can stop everything by closing your shell in the container and running:

```
devspace purge
```

and:

```
kubectl delete -f manifests/testing-namespace/
```

## Testing

The controller at each PR and push using [bats-detik](https://github.com/bats-core/bats-detik).

## Contributing

/* add CONTRIBUTING file at root */

## License

[MIT](https://choosealicense.com/licenses/mit/)
