const fs = require('fs');
const p = 'internal/shared/config/config.go';
let s = fs.readFileSync(p, 'utf8');
s = s.replace(/\tGatewayRuntimePreviousSecret\s+string\r?\n\tGatewayStaleAfter/, '\tGatewayRuntimePreviousSecret      string\n\tMetaCloudCredentialsJSON          string\n\tGatewayStaleAfter');
s = s.replace('" + '"GATEWAY_RUNTIME_SECRET_PREVIOUS", "MEDIA_DOWNLOAD_SECRET"' + "', '" + '"GATEWAY_RUNTIME_SECRET_PREVIOUS", "META_CLOUD_CREDENTIALS_JSON", "MEDIA_DOWNLOAD_SECRET"' + "');
s = s.replace(/\t\tGatewayRuntimePreviousSecret:\s+runtimePreviousSecret,\r?\n\t\tGatewayStaleAfter:/, '\t\tGatewayRuntimePreviousSecret:      runtimePreviousSecret,\n\t\tMetaCloudCredentialsJSON:          strings.TrimSpace(os.Getenv(\"META_CLOUD_CREDENTIALS_JSON\")),\n\t\tGatewayStaleAfter:');
fs.writeFileSync(p, s);
console.log('meta-config-hook-applied');
