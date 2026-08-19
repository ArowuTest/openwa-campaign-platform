const fs = require('fs');
const p = 'C:/Users/sanus/OpenWA/campaign-platform-active/repo/internal/dispatch/handler.go';
let s = fs.readFileSync(p, 'utf8');
const fields = '\tMetaSenderID                string\n\tMetaSenderVersion           int64\n\tMetaCredentialKey           string\n\tMetaGraphAPIVersion         string\n\tMetaPhoneNumberID           string\n\tMetaTemplateName            string\n\tMetaTemplateLanguage        string\n\tMetaBodyParameters          []string\n';
s = s.replace('\tRouteReference              string\n\tRecipientE164', '\tRouteReference              string\n' + fields + '\tRecipientE164');
const send = '\t\tMetaSenderID: material.MetaSenderID, MetaSenderVersion: material.MetaSenderVersion, MetaCredentialKey: material.MetaCredentialKey,\n\t\tMetaGraphAPIVersion: material.MetaGraphAPIVersion, MetaPhoneNumberID: material.MetaPhoneNumberID,\n\t\tMetaTemplateName: material.MetaTemplateName, MetaTemplateLanguage: material.MetaTemplateLanguage, MetaBodyParameters: append([]string(nil), material.MetaBodyParameters...),\n';
s = s.replace('\t\tAuthorityExpiresAt: material.AuthorityExpiresAt, RouteReference: material.RouteReference,\n', '\t\tAuthorityExpiresAt: material.AuthorityExpiresAt, RouteReference: material.RouteReference,\n' + send);
fs.writeFileSync(p, s);
console.log('META_DISPATCH_CONTRACT_FIELDS_APPLIED');
