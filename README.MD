# About

[![Release](https://github.com/embano1/vsphere-alarm-server/actions/workflows/release.yaml/badge.svg)](https://github.com/embano1/vsphere-alarm-server/actions/workflows/release.yaml)
[![Tests](https://github.com/embano1/vsphere-alarm-server/actions/workflows/unit-test.yaml/badge.svg)](https://github.com/embano1/vsphere-alarm-server/actions/workflows/unit-test.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/embano1/vsphere-alarm-server)](https://goreportcard.com/report/github.com/embano1/vsphere-alarm-server)
[![codecov](https://codecov.io/gh/embano1/vsphere-alarm-server/branch/main/graph/badge.svg?token=TC7MW723JO)](https://codecov.io/gh/embano1/vsphere-alarm-server)
[![go.mod Go version](https://img.shields.io/github/go-mod/go-version/embano1/vsphere-alarm-server)](https://github.com/embano1/vsphere-alarm-server)

Reacting to vSphere
[Alarms](https://docs.vmware.com/en/VMware-vSphere/6.7/com.vmware.vsphere.monitoring.doc/GUID-79AC1262-D701-4BC8-8F8D-F046AE0578CF.html)
via
[events](https://vdc-download.vmware.com/vmwb-repository/dcr-public/8946c1b6-2861-4c12-a45f-f14ae0d3b1b9/a5b8094c-c222-4307-9399-3b606a04af55/vim.event.AlarmEvent.html)
is a common requirement in building a resilient and scalable VMware vSphere
infrastructure. **Event-driven systems** are seeing increased adoption these
days due to their inherent nature of decoupling (sender/receiver), scalability
("fan out") and lower latency compared to polling over synchronous (blocking)
connections.

For example, an alarm can be created when a virtual machine is powered off or a
datastore is close to exceeding its capacity. However, when it comes to defining
actions executed when an alarm is triggered, VMware vCenter itself is rather
**limited** in its capabilities, only providing options for sending an
email/SNMP message or executing a script directly on the vCenter appliance. The
latter being a suboptimal solution due to security, scalability, resource and
extensibility concerns.

A more flexible solution is using an eventing system, like the VMware Event
Broker Appliance ([VEBA](https://vmweventbroker.io/)) or Knative
[Eventing](https://knative.dev/docs/eventing/). One can react to vSphere events
via user-provided functions (Function-as-a-Service) or forward events to a
persistence layer, e.g. database, Kafka messaging system, etc. for long-term
data retention, data transformation and analytics. Another benefit is that these
platforms use a **standard message** format
([CloudEvents](https://cloudevents.io/)) to simplify the creation/consumption of
events across platforms.

However, vSphere Alarm events pose a challenge for the end user: Alarm events do
not provide details on the underlying alarm definition, requiring the user
(function author) to write complex logic to retrieve additional metadata on the
alarm which triggered the event.

In addition, this leads to **increased load on vCenter** due to constantly
having to query for additional (alarm) information or writing custom caching
logic.

This project aims to tackle such common requirements by:

- Enriching vSphere Alarm events
  ([vim.event.Alarm*](https://vdc-download.vmware.com/vmwb-repository/dcr-public/8946c1b6-2861-4c12-a45f-f14ae0d3b1b9/a5b8094c-c222-4307-9399-3b606a04af55/vim.event.AlarmEvent.html))
  with metadata about the underlying alarm definition, e.g. defined thresholds
  and triggers
- Caching (with TTL expiration) alarm metadata to reduce load on vCenter and
  increase throughput
- Using CloudEvents as a standardized messaging format for ease of integration
  and extensibility

> **Note:** Currently only JSON-encoded event payloads (`datacontenttype="application/json"` or unset) are supported. See issue [#19](https://github.com/embano1/vsphere-alarm-server/issues/19).

## Event Flow

![alt](./static/flow.png)


## Example

### Regular AlarmStatusChangedEvent

The CloudEvent payload of a vSphere `AlarmStatusChangedEvent` provides useful
information about the alarm and associated vSphere objects:

```json
{
  "Key": 9300,
  "ChainId": 9300,
  "CreatedTime": "2021-04-10T20:49:30.032Z",
  "UserName": "VSPHERE.LOCAL\\Administrator",
  "Datacenter": {
    "Name": "vcqaDC",
    "Datacenter": {
      "Type": "Datacenter",
      "Value": "datacenter-2"
    }
  },
  "ComputeResource": {
    "Name": "cls",
    "ComputeResource": {
      "Type": "ClusterComputeResource",
      "Value": "domain-c7"
    }
  },
  "Host": {
    "Name": "10.192.193.184",
    "Host": {
      "Type": "HostSystem",
      "Value": "host-27"
    }
  },
  "Vm": {
    "Name": "test-01",
    "Vm": {
      "Type": "VirtualMachine",
      "Value": "vm-56"
    }
  },
  "Ds": null,
  "Net": null,
  "Dvs": null,
  "FullFormattedMessage": "Alarm 'power-off-alarm' on test-01 changed from Red to Green",
  "ChangeTag": "",
  "Alarm": {
    "Name": "power-off-alarm",
    "Alarm": {
      "Type": "Alarm",
      "Value": "alarm-283"
    }
  },
  "Source": {
    "Name": "test-01",
    "Entity": {
      "Type": "VirtualMachine",
      "Value": "vm-56"
    }
  },
  "Entity": {
    "Name": "test-01",
    "Entity": {
      "Type": "VirtualMachine",
      "Value": "vm-56"
    }
  },
  "From": "red",
  "To": "green"
}
```

> **Note:** Available vSphere Alarm event schemas and classes are described
> [here](https://vdc-download.vmware.com/vmwb-repository/dcr-public/8946c1b6-2861-4c12-a45f-f14ae0d3b1b9/a5b8094c-c222-4307-9399-3b606a04af55/vim.event.AlarmEvent.html).

### Enriched AlarmStatusChangedEvent

Using the vSphere Alarm Server, any vSphere Alarm event it receives
(configurable via `Trigger`, see below) will be enriched with the underlying
`AlarmInfo`, i.e. alarm definition. Note the injected `AlarmInfo` JSON object:

```json
{
  "Key": 9300,
  "ChainId": 9300,
  "CreatedTime": "2021-04-10T20:49:30.032Z",
  "UserName": "VSPHERE.LOCAL\\Administrator",
  "Datacenter": {
    "Name": "vcqaDC",
    "Datacenter": {
      "Type": "Datacenter",
      "Value": "datacenter-2"
    }
  },
  "ComputeResource": {
    "Name": "cls",
    "ComputeResource": {
      "Type": "ClusterComputeResource",
      "Value": "domain-c7"
    }
  },
  "Host": {
    "Name": "10.192.193.184",
    "Host": {
      "Type": "HostSystem",
      "Value": "host-27"
    }
  },
  "Vm": {
    "Name": "test-01",
    "Vm": {
      "Type": "VirtualMachine",
      "Value": "vm-56"
    }
  },
  "Ds": null,
  "Net": null,
  "Dvs": null,
  "FullFormattedMessage": "Alarm 'power-off-alarm' on test-01 changed from Red to Green",
  "ChangeTag": "",
  "Alarm": {
    "Name": "power-off-alarm",
    "Alarm": {
      "Type": "Alarm",
      "Value": "alarm-283"
    }
  },
  "Source": {
    "Name": "test-01",
    "Entity": {
      "Type": "VirtualMachine",
      "Value": "vm-56"
    }
  },
  "Entity": {
    "Name": "test-01",
    "Entity": {
      "Type": "VirtualMachine",
      "Value": "vm-56"
    }
  },
  "From": "red",
  "To": "green",
  "AlarmInfo": {
    "Name": "power-off-alarm",
    "SystemName": "",
    "Description": "Fired when VM is powered off",
    "Enabled": true,
    "Expression": {
      "Expression": [
        {
          "Comparisons": null,
          "EventType": "EventEx",
          "EventTypeId": "vim.event.VmPoweredOffEvent",
          "ObjectType": "VirtualMachine",
          "Status": "red"
        }
      ]
    },
    "Action": null,
    "ActionFrequency": 0,
    "Setting": {
      "ToleranceRange": 0,
      "ReportingFrequency": 300
    },
    "Key": "",
    "Alarm": {
      "Type": "Alarm",
      "Value": "alarm-283"
    },
    "Entity": {
      "Type": "VirtualMachine",
      "Value": "vm-56"
    },
    "LastModifiedTime": "2021-03-30T10:37:42.187061Z",
    "LastModifiedUser": "VSPHERE.LOCAL\\Administrator",
    "CreationEventId": 0
  }
}
```

> **Note:** The vSphere Alarm Server enriches the original event by patching the
> original event payload (AlarmEvent) and append a
> [configurable](#environment-variables) suffix to the CloudEvent `type`
> atttribute. Consumers, e.g. functions, can subscribe or filter for this
> specific event `type` to ignore the original (unmodified) events and avoid
> multiple invocations on the same alarm event.

# Installation

## Requirements

- VMware Event Broker Appliance with the
  [Knative](https://vmweventbroker.io/kb/install-knative) event processor
  connected to a vCenter
- `kubectl`

> **Note:** Alternatively, the deployment could be made directly on Knative
> Eventing using the [VMware Tanzu Sources for
> Knative](https://github.com/vmware-tanzu/sources-for-knative). However,
> currently the sources only emit XML encoded vSphere events which this project
> does not support. An
> [issue](https://github.com/vmware-tanzu/sources-for-knative/issues/222) is
> open to track this limitation.

## Deploy from Release

Create a secret holding your vCenter credentials using `kubectl` targeting your
VEBA deployment:

> **Note:** The examples use the `vmare-system` namespace because it has the
> required `default` broker already set up.

```
kubectl -n vmware-functions create secret generic vsphere-credentials --from-literal=username=administrator@vsphere.local --from-literal=password=passw0rd
```

> **Note:** The credentials are used by the vSphere Alarm Server to retrieve
> alarm definitions. Thus, a read-only role is sufficient. If you scope the role
> to a particular cluster/inventory objects, the server might not be able to
> retrieve all alarm definitions for the incoming events.


Next, download the latest [release manifest](https://github.com/embano1/vsphere-alarm-server/releases/latest/download/release.yaml)
for the Kubernetes objects, e.g. via `curl`:

```
curl -L -O https://github.com/embano1/vsphere-alarm-server/releases/latest/download/release.yaml
```

The manifest contains the following objects:

- Deployment for the vSphere Alarm Server
- Secret for the vSphere Alarm Server to retrieve vSphere Alarm definitions
- Trigger to customize which events to send to the vSphere Alarm Server

### Configure the vSphere Alarm Server

Change the environment variables in the `deployment` according to your setup
(see [documentation](#environment-variables) below). 

### Configure the Secret

If you followed the `kubectl create secret` command above, no change is needed.
Otherwise change the name in the YAML manifest to match your setup.

### Configure the Trigger

The broker in VEBA in the `vmware-functions` namespace is called `default`, thus no change is required in the provided `Trigger` YAML manifest. Otherwise change this line accordingly:

```yaml
spec:
  broker: default
```

### Deploy the application components:

```
kubectl -n vmware-functions apply -f release.yaml
```

Verify all components are up and running:


```yaml
kubectl -n vmware-functions get all -l app=vsphere-alarm-server
NAME                                        READY   STATUS    RESTARTS   AGE
pod/vsphere-alarm-server-7cf9756785-mj9sf   1/1     Running   0          1m

NAME                           TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)   AGE
service/vsphere-alarm-server   ClusterIP   10.96.44.213   <none>        80/TCP    1m

NAME                                   READY   UP-TO-DATE   AVAILABLE   AGE
deployment.apps/vsphere-alarm-server   1/1     1            1           1m

NAME                                              DESIRED   CURRENT   READY   AGE
replicaset.apps/vsphere-alarm-server-7cf9756785   1         1         1       1m

NAME                                                BROKER    SUBSCRIBER_URI                                                    AGE  READY   REASON
trigger.eventing.knative.dev/vsphere-alarm-server   default   http://vsphere-alarm-server.vmware-functions.svc.cluster.local/   1m   True
```

#### Troubleshooting: vSphere Alarm Server is in CrashLoop backoff

Check the logs of the deployment for errors:

```
kubectl -n vmware-functions logs deploy/vsphere-alarm-server
```

#### Troubleshooting: Trigger is not showing "Ready"

Check the trigger definition for issues:


```
kubectl -n vmware-functions describe trigger vsphere-alarm-server
```

## Environment Variables

The vSphere Alarm Server is configured through environment variables (in the
Kubernetes `deployment` manifest) instead of using command line flags.

| Variable            | Description                                                                                                   | Default                 | Required |
|---------------------|---------------------------------------------------------------------------------------------------------------|-------------------------|----------|
| PORT                | Listen port for the server                                                                                    | 8080                    | yes      |
| CACHE_TTL           | Time-to-live for alarm objects in the cache before requesting update from vCenter                             | 3600 (seconds)          | no       |
| VCENTER_URL         | URI of vCenter to connect to (https://vcenter.corp.local)                                                     | (empty)                 | yes      |
| VCENTER_INSECURE    | Ignore TLS certificate warnings when connecting to vCenter                                                    | "false"                 | no       |
| VCENTER_SECRET_PATH | Where to mount the injected vSphere Kubernetes secret credentials                                             | "/var/bindings/vsphere" | yes      |
| DEBUG               | Print debug log statements                                                                                    | "false"                 | no       |
| EVENT_SUFFIX        | Suffix to append to the CloudEvents `type`, e.g. "AlarmInfo"                                                  | (empty)                 | yes      |
| ALARM_KEY           | Injected JSON key into the CloudEvents `data` (payload) representing the alarm info details, e.g. "AlarmInfo" | (empty)                 | yes      |

### Example EVENT_SUFFIX

If the incoming CloudEvent `type` is `com.vmware.event.router/event` and the
event `data` is a class of AlarmEvent the returned event type using
`EVENT_SUFFIX="AlarmInfo"` would be `com.vmware.event.router/event.AlarmInfo`.

## Build Custom Image

**Note:** This step is only required if you made code changes to the Go code.

This example uses [`ko`](https://github.com/google/ko) to build and push
container artifacts.

```console
# only when using kind: 
# export KIND_CLUSTER_NAME=kind
# export KO_DOCKER_REPO=kind.local

export KO_DOCKER_REPO=my-docker-username
export KO_COMMIT=$(git rev-parse --short=8 HEAD)
export KO_TAG=$(git describe --abbrev=0 --tags)

# build, push and run the worker in the configured Kubernetes context 
# and vmware-preemption Kubernetes namespace
ko resolve -BRf config | kubectl -n vmware-functions apply -f -
```

To delete the deployment:

```console
ko -n vmware-functions delete -f config
```
