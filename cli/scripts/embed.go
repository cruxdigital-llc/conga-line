package scripts

import _ "embed"

//go:embed add-user.sh.tmpl
var AddUserScript string

//go:embed refresh-user.sh.tmpl
var RefreshUserScript string

//go:embed remove-user.sh.tmpl
var RemoveUserScript string
