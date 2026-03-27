#!/bin/sh
exec envoy -c /etc/envoy/envoy.yaml --log-level warn
