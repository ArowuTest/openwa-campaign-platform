const fs = require('fs');
const p = 'C:/Users/sanus/OpenWA/campaign-platform-active/repo/internal/execution/routing_meta_postgres_integration_test.go';
let s = fs.readFileSync(p, 'utf8');
const old = `\tif loaded.DistributionMode != DistributionWeighted || len(loaded.Routes) != 2 || loaded.Routes[1].MetaSenderID == "" || loaded.Routes[1].MetaSenderVersion != 4 {\n\t\tt.Fatalf("Meta routing evidence was not persisted: %#v", loaded)\n\t}`;
const neu = `\tvar loadedMeta PoolRoute\n\tfor _, route := range loaded.Routes {\n\t\tif route.Provider == "META" { loadedMeta = route }\n\t}\n\tif loaded.DistributionMode != DistributionWeighted || len(loaded.Routes) != 2 || loadedMeta.MetaSenderID != metaSenderID || loadedMeta.MetaSenderVersion != 4 {\n\t\tt.Fatalf("Meta routing evidence was not persisted: %#v", loaded)\n\t}`;
if (!s.includes(old)) throw new Error('route order assertion target not found');
fs.writeFileSync(p, s.replace(old, neu));
console.log('META_ROUTING_TEST_ORDER_FIXED');
