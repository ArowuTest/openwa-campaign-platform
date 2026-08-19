const fs = require('fs');
const p = 'C:/Users/sanus/OpenWA/campaign-platform-active/repo/internal/dispatch/meta_material_postgres_integration_test.go';
let s = fs.readFileSync(p, 'utf8');
const old = `\tif _, err := db.ExecContext(ctx, \`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,maximum_messages_per_recipient,approved_message_version_id,required_capabilities) VALUES($1::uuid,$2::uuid,'Meta material',$3::uuid,'SCHEDULED',1,1,$4::uuid,'["SEND_TEXT"]'::jsonb)\`, campaignID, orgID, purposeID, messageID); err != nil {\n\t\t// approved message FK is populated after message creation below.\n\t\tif _, resetErr := db.ExecContext(ctx, \`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,maximum_messages_per_recipient,required_capabilities) VALUES($1::uuid,$2::uuid,'Meta material',$3::uuid,'SCHEDULED',1,1,'["SEND_TEXT"]'::jsonb)\`, campaignID, orgID, purposeID); resetErr != nil { t.Fatal(resetErr) }\n\t}`;
const neu = `\tif _, err := db.ExecContext(ctx, \`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,maximum_messages_per_recipient,required_capabilities) VALUES($1::uuid,$2::uuid,'Meta material',$3::uuid,'SCHEDULED',1,1,'["SEND_TEXT"]'::jsonb)\`, campaignID, orgID, purposeID); err != nil { t.Fatal(err) }`;
if (!s.includes(old)) throw new Error('campaign fixture block not found');
fs.writeFileSync(p, s.replace(old, neu));
console.log('META_MATERIAL_FIXTURE_CLEANED');
