package apistruct

type LinkPreviewReq struct {
	URL string `json:"url" binding:"required"`
}

type LinkPreviewResp struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	ImageURL    string `json:"imageUrl"`
	SiteName    string `json:"siteName"`
}
