package backupdto

type RestoreInfo struct {
	BaseTar         string
	TablespacesTars []string
	ManifestFile    string
}
