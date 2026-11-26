package configfile

const (
	QueueActiveFirst  = "active-first"
	QueueNameAsc      = "name-asc"
	QueueNameDesc     = "name-desc"
	QueueProgressAsc  = "progress-asc"
	QueueProgressDesc = "progress-desc"
	QueueStateAsc     = "state-asc"
	QueueStateDesc    = "state-desc"
	QueueSizeDesc     = "size-desc"
	QueueRatioDesc    = "ratio-desc"
	QueueETAAsc       = "eta-asc"
	QueueDownloadDesc = "download-speed-desc"
	QueueUploadDesc   = "upload-speed-desc"

	QueueColumnsCompact = "compact"
	QueueColumnsWide    = "wide"
)

func QueueSortValues() []string {
	return []string{QueueActiveFirst, QueueNameAsc, QueueNameDesc, QueueProgressAsc, QueueProgressDesc, QueueStateAsc, QueueStateDesc, QueueSizeDesc, QueueRatioDesc, QueueETAAsc, QueueDownloadDesc, QueueUploadDesc}
}
