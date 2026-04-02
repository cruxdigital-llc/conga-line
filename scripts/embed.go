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

//go:embed refresh-all.sh.tmpl
var RefreshAllScript string

//go:embed pause-agent.sh.tmpl
var PauseAgentScript string

//go:embed unpause-agent.sh.tmpl
var UnpauseAgentScript string

//go:embed deploy-egress.sh.tmpl
var DeployEgressScript string
