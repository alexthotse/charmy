package models

const (
	A2AGeneric ModelID = "a2a.generic"
)

var A2AModels = map[ModelID]Model{
	A2AGeneric: {
		ID:           A2AGeneric,
		Name:         "A2A Generic",
		Provider:     ProviderA2A,
		APIModel:     "a2a.generic",
		CostPer1MIn:  0,
		CostPer1MOut: 0,
	},
}
