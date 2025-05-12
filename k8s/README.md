## A brief example of usage

### Prerequisites:

You can use a Kind cluster for testing. If you haven’t installed it yet, check out the [Kind installation guide](https://kind.sigs.k8s.io/).

### Deploy:

Execute scripts one by one:
```
# prepare kind cluster
bash 00-setup-kind.sh

# deploy 
bash 01-deploy.sh
```

### Verification:

```
curl --location 'http://localhost:30266/status' --header 'Authorization: Bearer pgrwladmin'
```
