const fs = require('fs');
const p = 'internal/metacloud/template_client.go';
let s = fs.readFileSync(p, 'utf8');
s = s.replace('\t"strings"\n)', '\t"strings"\n\t"time"\n)');
fs.writeFileSync(p, s);
console.log('template-client-time-import-applied');
