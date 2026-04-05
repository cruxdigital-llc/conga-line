package openclaw

import "encoding/json"

func (r *Runtime) ReadGatewayToken(configData []byte) string {
	var config map[string]interface{}
	if err := json.Unmarshal(configData, &config); err != nil {
		return ""
	}
	if gw, ok := config["gateway"].(map[string]interface{}); ok {
		if auth, ok := gw["auth"].(map[string]interface{}); ok {
			if t, ok := auth["token"].(string); ok {
				return t
			}
		}
		if t, ok := gw["token"].(string); ok {
			return t
		}
	}
	return ""
}

func (r *Runtime) GatewayTokenDockerExec() []string {
	return []string{
		"node", "-e",
		`try{const c=require('/home/node/.openclaw/openclaw.json');` +
			`console.log(c.gateway?.token||c.gateway?.auth?.token||'')}catch(e){console.log('')}`,
	}
}
