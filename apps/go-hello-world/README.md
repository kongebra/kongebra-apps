# go-hello-world

Minimal Go HTTP-server som returnerer `Hello World` på `/`. Test-app for Dokploy-deploy.

## Lokalt

```sh
go run .          # PORT=8080 default
curl localhost:8080/
```

## Dokploy

Build Type = Dockerfile, container-port `8080`. PORT settes av plattformen.
