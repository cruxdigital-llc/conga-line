package scripts

import _ "embed"

//go:embed add-user.sh.tmpl
var AddUserScript string

//go:embed add-team.sh.tmpl
var AddTeamScript string

//go:embed refresh-user.sh.tmpl
var RefreshUserScript string

//go:embed remove-agent.sh.tmpl
var RemoveAgentScript string
