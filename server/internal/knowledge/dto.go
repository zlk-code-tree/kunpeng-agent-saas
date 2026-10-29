package knowledge

type KnowledgeCreateInput struct {
	Name       string `json:"name" binding:"required,min=2,max=100"`
	Visibility string `json:"visibility" binding:"required,oneof=private tenant"`
}
