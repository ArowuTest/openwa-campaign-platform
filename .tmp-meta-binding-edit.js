const fs = require('fs');
const p = 'internal/metacloud/template.go';
let s = fs.readFileSync(p, 'utf8');
const old = '\tif strings.ToUpper(strings.TrimSpace(binding.MediaHeaderType)) != expectedHeader {\n\t\treturn ErrBindingInvalid\n\t}\n\treturn nil\n}';
const neu = '\tif strings.ToUpper(strings.TrimSpace(binding.MediaHeaderType)) != expectedHeader {\n\t\treturn ErrBindingInvalid\n\t}\n\ttemplateHeader, err := templateMediaHeaderType(template.Components)\n\tif err != nil || templateHeader != expectedHeader {\n\t\treturn ErrBindingInvalid\n\t}\n\treturn nil\n}';
if (!s.includes(old)) throw new Error('binding validation target not found');
s = s.replace(old, neu);
fs.writeFileSync(p, s);
console.log('binding-media-validation-applied');
