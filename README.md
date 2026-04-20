# glance-addon

A small HTTP server that surfaces pending [Renovate](https://github.com/renovatebot/renovate) issues as a [Glance](https://github.com/glanceapp/glance) extension widget.

## How it works

Renovate opens a dependency dashboard issue in each tracked repository. This server queries the GitHub Issues API for open issues created by `renovate[bot]` and returns an HTML fragment that Glance renders as a widget.

## Configuration

| Env var | Required | Description |
|---|---|---|
| `GITHUB_TOKEN` | yes | Fine-grained PAT with **Issues: Read-only** on each repo |
| `REPOS` | yes | Comma-separated list of `org/repo` to watch |
| `PORT` | no | Port to listen on (default `8080`) |

## Glance widget

```yaml
- type: extension
  title: Renovate
  url: http://glance-addon:8080
  cache: 30m
  allow-potentially-dangerous-html: true
```

## Running with Docker

```sh
docker run -e GITHUB_TOKEN=... -e REPOS=org/repo1,org/repo2 -p 8080:8080 ghcr.io/duchaineo1/glance-addon:latest
```

## Deploying to Kubernetes

The `k8s-configs` manifests expect:
- A secret named `glance-addon-secret` with key `github-token` in the `glance` namespace
- The `REPOS` env var set in `addon-deployment.yaml`
