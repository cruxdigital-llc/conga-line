package templates

import _ "embed"

//go:embed envoy-config.yaml.tmpl
var EnvoyConfig string

//go:embed proxy-bootstrap.js
var ProxyBootstrapJS string

//go:embed proxy-entrypoint.sh
var ProxyEntrypoint string
