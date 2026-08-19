const fs = require('fs');
const p = 'internal/metacloud/template_client.go';
let s = fs.readFileSync(p, 'utf8');
s = s.replace('QualityScore string          `json:"quality_score"`', 'QualityScore json.RawMessage `json:"quality_score"`');
s = s.replace('QualitySignal: strings.TrimSpace(item.QualityScore), Components:', 'QualitySignal: decodeQualitySignal(item.QualityScore), Components:');
fs.writeFileSync(p, s);
console.log('template-client-quality-edit-applied');
