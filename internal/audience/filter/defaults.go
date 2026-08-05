package filter

func DefaultDefinitions() []Definition {
	return []Definition{
		{
			Code: "COUNTRY", DisplayName: "Country", Description: "Country associated with the contact profile.",
			DataType: DataTypeGeography, Operators: []Operator{OperatorIn, OperatorNotIn, OperatorIsKnown, OperatorIsUnknown},
			Core: true, Storage: StorageCoreColumn, Filterable: true, Reportable: true, QueryableField: "c.country_id", IndexStrategy: "btree", DisplayOrder: 10, Active: true,
		},
		{
			Code: "STATE", DisplayName: "State or region", Description: "State, province or first-level administrative region.",
			DataType: DataTypeGeography, Operators: []Operator{OperatorIn, OperatorNotIn, OperatorIsKnown, OperatorIsUnknown},
			Core: true, Storage: StorageCoreColumn, Filterable: true, Reportable: true, QueryableField: "c.state_id", IndexStrategy: "btree", DisplayOrder: 20, Active: true,
		},
		{
			Code: "LGA", DisplayName: "LGA or district", Description: "Local government area, district or second-level administrative region.",
			DataType: DataTypeGeography, Operators: []Operator{OperatorIn, OperatorNotIn, OperatorIsKnown, OperatorIsUnknown},
			Core: true, Storage: StorageCoreColumn, Filterable: true, Reportable: true, QueryableField: "c.lga_id", IndexStrategy: "btree", DisplayOrder: 30, Active: true,
		},
		{
			Code: "REPORTED_AGE", DisplayName: "Reported age", Description: "Age supplied by the contact when the source record was collected.",
			DataType: DataTypeInteger, Operators: []Operator{OperatorEquals, OperatorBetween, OperatorGreaterThan, OperatorLessThan, OperatorIsKnown, OperatorIsUnknown},
			Core: true, Storage: StorageCoreColumn, Filterable: true, Reportable: true, Sensitive: true, RequiresPermission: "audience.demographics.read", QueryableField: "c.reported_age", IndexStrategy: "btree", DisplayOrder: 40, Active: true,
		},
		{
			Code: "AGE_RECORDED_AT", DisplayName: "Age recorded date", Description: "Date on which the self-declared age was collected.",
			DataType: DataTypeDate, Operators: []Operator{OperatorEquals, OperatorBetween, OperatorGreaterThan, OperatorLessThan, OperatorIsKnown, OperatorIsUnknown},
			Core: true, Storage: StorageCoreColumn, Filterable: true, Reportable: true, Sensitive: true, RequiresPermission: "audience.demographics.read", QueryableField: "c.age_recorded_at", IndexStrategy: "btree", DisplayOrder: 45, Active: true,
		},
		{
			Code: "GENDER", DisplayName: "Gender", Description: "Optional self-declared gender value.",
			DataType: DataTypeSingleSelect, Operators: []Operator{OperatorIn, OperatorNotIn, OperatorIsKnown, OperatorIsUnknown},
			Core: true, Storage: StorageCoreColumn, Filterable: true, Reportable: true, Sensitive: true, RequiresPermission: "audience.demographics.read", QueryableField: "c.gender_code", IndexStrategy: "btree", DisplayOrder: 50, Active: true,
		},
		{
			Code: "CONSENT_PURPOSE", DisplayName: "Consent purpose", Description: "The approved marketing purpose covered by the active consent grant.",
			DataType: DataTypeMultiSelect, Operators: []Operator{OperatorIn, OperatorNotIn},
			Core: true, Storage: StorageCoreColumn, Filterable: true, Reportable: true, QueryableField: "cg.purpose_id", IndexStrategy: "composite-partial", DisplayOrder: 60, Active: true,
		},
		{
			Code: "PREFERRED_LANGUAGE", DisplayName: "Preferred language", Description: "Preferred language supplied with the contact record.",
			DataType: DataTypeSingleSelect, Operators: []Operator{OperatorIn, OperatorNotIn, OperatorIsKnown, OperatorIsUnknown},
			Core: true, Storage: StorageCoreColumn, Filterable: true, Reportable: true, QueryableField: "c.preferred_language_code", IndexStrategy: "btree", DisplayOrder: 70, Active: true,
		},
	}
}
