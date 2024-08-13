# cache

[![GoDoc](https://godoc.org/golift.io/cache/svc?status.svg)](https://pkg.go.dev/golift.io/cache)
[![Go Report Card](https://goreportcard.com/badge/golift.io/cache)](https://goreportcard.com/report/golift.io/cache)
[![MIT License](http://img.shields.io/:license-mit-blue.svg)](https://github.com/golift/cache/blob/master/LICENSE)
[![discord](https://badgen.net/badge/icon/Discord?color=0011ff&label&icon=https://simpleicons.now.sh/discord/eee "GoLift Discord")](https://golift.io/discord)

This go module provides a very simple in-memory key/value cache.
It uses 1 mutex lock only during start and stop; utilizes only 1 go routine,
2 channels, and 1 or 2 tickers depending on if you enable the pruner.
The module also exports git/miss statistics that you can 
plug into expvar, or other metrics modules.

This module has a concept of pruning. Items can be marked prunable,
or not prunable. Those marked prunable are deleted after they have not
had a `get` request within a specified duration. Those marked not-prunable
have a different configurable maximum unused age.

I wrote this to cache data from mysql queries for an [nginx auth proxy](https://github.com/Notifiarr/mysql-auth-proxy).
I've since began using it in plenty of other places as a global data store.
See a simple example in [cache_test.go](cache_test.go).
