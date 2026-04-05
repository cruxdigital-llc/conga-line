package hermes

import "gopkg.in/yaml.v3"

func (r *Runtime) ReadGatewayToken(configData []byte) string {
	var config map[string]any
	if err := yaml.Unmarshal(configData, &config); err != nil {
		return ""
	}
	if platforms, ok := config["platforms"].(map[string]any); ok {
		if apiServer, ok := platforms["api_server"].(map[string]any); ok {
			// Token may be in extra.key (config.yaml structure)
			if extra, ok := apiServer["extra"].(map[string]any); ok {
				if key, ok := extra["key"].(string); ok {
					return key
				}
			}
			// Or directly on api_server.key (flat structure)
			if key, ok := apiServer["key"].(string); ok {
				return key
			}
		}
	}
	return ""
}

func (r *Runtime) GatewayTokenDockerExec() []string {
	return []string{
		"python3", "-c",
		`import yaml; c=yaml.safe_load(open('/opt/data/config.yaml')); print(c.get('platforms',{}).get('api_server',{}).get('key',''))`,
	}
}
