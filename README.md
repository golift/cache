# cache

[![GoDoc](https://godoc.org/golift.io/cache/svc?status.svg)](https://pkg.go.dev/golift.io/cache)
[![Go Report Card](https://goreportcard.com/badge/golift.io/cache)](https://goreportcard.com/report/golift.io/cache)
[![MIT License](http://img.shields.io/:license-mit-blue.svg)](https://github.com/golift/cache/blob/master/LICENSE)
[![discord](https://badgen.net/badge/icon/Discord?color=0011ff&label&icon=https://simpleicons.now.sh/discord/eee "GoLift Discord")](https://golift.io/discord)

This go module provides a very simple in-memory go cache.
It uses no locks; only channels to accomplish the caching.
The module also exports git/miss statistics that you can 
plug into expvar, or other metrics plugins.

This module has a concept of pruning. Items can be marked prunable,
or not prunable. Those marked prunable are deleted after they have not
had a `get` requst within a specified duration. Those marked not-prunable
have a different configurable maximum unused age.

I wrote this to cache data from mysql queries for an [nginx auth proxy](https://github.com/Notifiarr/mysql-auth-proxy).
See a simple example in [cache_example_test.go](cache_example_test.go).