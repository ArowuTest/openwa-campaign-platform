const fs = require('fs');
const p = 'C:/Users/sanus/OpenWA/campaign-platform-active/repo/internal/dispatch/handler.go';
let s = fs.readFileSync(p, 'utf8');
const start = s.indexOf('func validateMaterial(v Material) error {');
const end = s.indexOf('\nfunc safeError(', start);
if (start < 0 || end < 0) throw new Error('validateMaterial block not found');
const block = `func validateMaterial(v Material) error {
	provider := strings.ToUpper(strings.TrimSpace(v.Provider))
	engine := strings.ToUpper(strings.TrimSpace(v.Engine))
	if strings.TrimSpace(v.RouteReference) == "" || !strings.HasPrefix(strings.TrimSpace(v.RecipientE164), "+") {
		return errors.New("dispatch material requires route reference and E.164 recipient")
	}
	switch provider {
	case "OPENWA":
		if (engine != "WHATSAPP_WEB_JS" && engine != "BAILEYS") || strings.TrimSpace(v.GatewayPoolID) == "" ||
			v.GatewayPoolVersion <= 0 || strings.TrimSpace(v.GatewayAdapterVersion) == "" || strings.TrimSpace(v.GatewayNodeID) == "" ||
			v.GatewayNodeVersion <= 0 || strings.TrimSpace(v.SessionID) == "" || v.SessionLeaseVersion <= 0 ||
			v.SessionConfigurationVersion <= 0 || v.AuthorityExpiresAt.IsZero() {
			return errors.New("OpenWA dispatch material requires fenced gateway session authority")
		}
	case "META":
		if engine != "CLOUD_API" || strings.TrimSpace(v.MetaSenderID) == "" || v.MetaSenderVersion <= 0 ||
			strings.TrimSpace(v.MetaCredentialKey) == "" || strings.TrimSpace(v.MetaGraphAPIVersion) == "" ||
			strings.TrimSpace(v.MetaPhoneNumberID) == "" || strings.TrimSpace(v.MetaTemplateName) == "" || strings.TrimSpace(v.MetaTemplateLanguage) == "" {
			return errors.New("Meta dispatch material requires governed sender and template evidence")
		}
		if strings.TrimSpace(v.GatewayPoolID) != "" || strings.TrimSpace(v.GatewayNodeID) != "" || strings.TrimSpace(v.SessionID) != "" {
			return errors.New("Meta dispatch material cannot carry OpenWA gateway authority")
		}
	default:
		return errors.New("unsupported dispatch provider")
	}
	if v.MessageType == "text" && strings.TrimSpace(v.Body) == "" {
		return errors.New("text dispatch requires body")
	}
	if v.MessageType != "text" && strings.TrimSpace(v.MediaObjectURL) == "" {
		return errors.New("media dispatch requires object URL")
	}
	return nil
}`;
s = s.slice(0, start) + block + s.slice(end);
fs.writeFileSync(p, s);
console.log('META_MATERIAL_PROVIDER_VALIDATION_APPLIED');
