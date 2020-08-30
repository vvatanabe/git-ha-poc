# git-ha-poc ![Go](https://github.com/vvatanabe/git-ha-poc/workflows/Go/badge.svg)

Just a proof of concept for a highly available Git servers.

## Overview

![overview](https://cacoo.com/diagrams/9BhTPbZUoMwmP1zp-7076D.png)

## Requirements

Go 1.14+

## Works

### Build each services

```
$ docker-compose build
```

### Run each services

```
$ docker-compose up
```

### Create a repository

```
$ curl -X POST localhost:8080/foo/test.git
```

### Clone a repository

```
# HTTP
$ git clone http://localhost:8080/foo/test.git

# SSH
$ git clone ssh://git@localhost:2222/foo/test.git
```

## Acknowledgments

- [mwitkow/grpc-proxy](https://github.com/mwitkow/grpc-proxy)
- [mwitkow/grpc-proxy forks](https://github.com/mwitkow/grpc-proxy/network/members)
- [Gitaly Cluster](https://docs.gitlab.com/ee/administration/gitaly/praefect.html)
- [Designing Data-Intensive Applications](https://www.oreilly.com/library/view/designing-data-intensive-applications/9781491903063/)（[データ指向アプリケーションデザイン](https://www.oreilly.co.jp/books/9784873118703/)）
- [分散システム (第2版)](https://www.kyoritsu-pub.co.jp/bookdetail/9784320124493)

## Author

[vvatanabe](https://github.com/vvatanabe)
