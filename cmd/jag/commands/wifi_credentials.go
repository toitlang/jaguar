package commands

import (
	"strings"

	"github.com/spf13/viper"
)

type wifiCredential struct {
	SSID     string `mapstructure:"ssid" json:"ssid" yaml:"ssid"`
	Password string `mapstructure:"password" json:"password" yaml:"password"`
}

func (c wifiCredential) normalized() wifiCredential {
	return wifiCredential{
		SSID:     strings.TrimSpace(c.SSID),
		Password: c.Password,
	}
}

func (c wifiCredential) isValid() bool {
	return strings.TrimSpace(c.SSID) != ""
}

func loadWifiCredentials(cfg *viper.Viper) []wifiCredential {
	if cfg == nil || !cfg.IsSet(WifiCfgKey) {
		return nil
	}
	creds := parseWifiConfigValue(cfg.Get(WifiCfgKey))
	return normalizeWifiCredentials(creds)
}

func parseWifiConfigValue(value interface{}) []wifiCredential {
	switch v := value.(type) {
	case nil:
		return nil
	case wifiCredential:
		if v.isValid() {
			return []wifiCredential{v.normalized()}
		}
	case map[string]string:
		cred := wifiCredential{
			SSID:     v[WifiSSIDCfgKey],
			Password: v[WifiPasswordCfgKey],
		}
		if cred.isValid() {
			return []wifiCredential{cred.normalized()}
		}
	case map[string]interface{}:
		if cred, ok := credentialFromMap(v); ok {
			return []wifiCredential{cred}
		}
	case map[interface{}]interface{}:
		if cred, ok := credentialFromInterfaceMap(v); ok {
			return []wifiCredential{cred}
		}
	case []interface{}:
		res := make([]wifiCredential, 0, len(v))
		for _, item := range v {
			if cred, ok := wifiCredentialFromValue(item); ok {
				res = append(res, cred)
			}
		}
		return res
	case []wifiCredential:
		res := make([]wifiCredential, 0, len(v))
		for _, item := range v {
			if item.isValid() {
				res = append(res, item.normalized())
			}
		}
		return res
	case []map[string]string:
		res := make([]wifiCredential, 0, len(v))
		for _, item := range v {
			cred := wifiCredential{
				SSID:     item[WifiSSIDCfgKey],
				Password: item[WifiPasswordCfgKey],
			}
			if cred.isValid() {
				res = append(res, cred.normalized())
			}
		}
		return res
	case []map[string]interface{}:
		res := make([]wifiCredential, 0, len(v))
		for _, item := range v {
			if cred, ok := credentialFromMap(item); ok {
				res = append(res, cred)
			}
		}
		return res
	case []map[interface{}]interface{}:
		res := make([]wifiCredential, 0, len(v))
		for _, item := range v {
			if cred, ok := credentialFromInterfaceMap(item); ok {
				res = append(res, cred)
			}
		}
		return res
	}
	return nil
}

func wifiCredentialFromValue(value interface{}) (wifiCredential, bool) {
	switch entry := value.(type) {
	case wifiCredential:
		if entry.isValid() {
			return entry.normalized(), true
		}
	case map[string]string:
		cred := wifiCredential{
			SSID:     entry[WifiSSIDCfgKey],
			Password: entry[WifiPasswordCfgKey],
		}
		if cred.isValid() {
			return cred.normalized(), true
		}
	case map[string]interface{}:
		if cred, ok := credentialFromMap(entry); ok {
			return cred, true
		}
	case map[interface{}]interface{}:
		if cred, ok := credentialFromInterfaceMap(entry); ok {
			return cred, true
		}
	case []interface{}:
		// Unsupported nested list.
		return wifiCredential{}, false
	case string:
		if strings.TrimSpace(entry) == "" {
			return wifiCredential{}, false
		}
		return wifiCredential{SSID: strings.TrimSpace(entry)}, true
	}
	return wifiCredential{}, false
}

func credentialFromMap(m map[string]interface{}) (wifiCredential, bool) {
	ssid := stringFromInterface(m[WifiSSIDCfgKey])
	password := stringFromInterface(m[WifiPasswordCfgKey])
	cred := wifiCredential{SSID: ssid, Password: password}
	if !cred.isValid() {
		return wifiCredential{}, false
	}
	return cred.normalized(), true
}

func credentialFromInterfaceMap(m map[interface{}]interface{}) (wifiCredential, bool) {
	converted := make(map[string]interface{}, len(m))
	for key, value := range m {
		keyStr, ok := key.(string)
		if !ok {
			continue
		}
		converted[keyStr] = value
	}
	return credentialFromMap(converted)
}

func stringFromInterface(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case nil:
		return ""
	default:
		return ""
	}
}

func normalizeWifiCredentials(creds []wifiCredential) []wifiCredential {
	if len(creds) == 0 {
		return nil
	}
	res := make([]wifiCredential, 0, len(creds))
	seen := make(map[string]bool, len(creds))
	for _, cred := range creds {
		if !cred.isValid() {
			continue
		}
		normalized := cred.normalized()
		if seen[normalized.SSID] {
			continue
		}
		seen[normalized.SSID] = true
		res = append(res, normalized)
	}
	if len(res) == 0 {
		return nil
	}
	return res
}

func upsertWifiCredential(creds []wifiCredential, cred wifiCredential) []wifiCredential {
	normalized := cred.normalized()
	if !normalized.isValid() {
		return normalizeWifiCredentials(creds)
	}
	creds = normalizeWifiCredentials(creds)
	replaced := false
	for index, existing := range creds {
		if existing.SSID == normalized.SSID {
			creds[index] = normalized
			replaced = true
			break
		}
	}
	if !replaced {
		creds = append(creds, normalized)
	}
	return creds
}

func removeWifiCredential(creds []wifiCredential, ssid string) ([]wifiCredential, bool) {
	target := strings.TrimSpace(ssid)
	if target == "" {
		return normalizeWifiCredentials(creds), false
	}
	creds = normalizeWifiCredentials(creds)
	result := make([]wifiCredential, 0, len(creds))
	removed := false
	for _, cred := range creds {
		if cred.SSID == target {
			removed = true
			continue
		}
		result = append(result, cred)
	}
	if len(result) == 0 {
		return nil, removed
	}
	return result, removed
}

func saveWifiCredentials(cfg *viper.Viper, creds []wifiCredential) {
	normalized := normalizeWifiCredentials(creds)
	if len(normalized) == 0 {
		cfg.Set(WifiCfgKey, []map[string]string{})
		return
	}
	serialized := make([]map[string]string, 0, len(normalized))
	for _, cred := range normalized {
		serialized = append(serialized, map[string]string{
			WifiSSIDCfgKey:     cred.SSID,
			WifiPasswordCfgKey: cred.Password,
		})
	}
	cfg.Set(WifiCfgKey, serialized)
}

func findWifiCredential(creds []wifiCredential, ssid string) (wifiCredential, bool) {
	target := strings.TrimSpace(ssid)
	for _, cred := range normalizeWifiCredentials(creds) {
		if cred.SSID == target {
			return cred, true
		}
	}
	return wifiCredential{}, false
}
