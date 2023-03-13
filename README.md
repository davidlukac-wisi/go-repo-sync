# Repo Sync

Simple Golang script using `go-git` to sync repositories from branch to another. Input YAML
should be provided as script argument.

Example input:
```yaml
---
repos:
  foo:
    path: local/repos/foo
    sourceRemote:
      name: origin
    targetRemote:
      name: github
      url: git@github.com:bar/foo.git
branchMapping:
  master: main
```
