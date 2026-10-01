package f3411

// Constants of ASTM F3411-22a, copied with their names and values from
// uas_standards src/uas_standards/astm/f3411/v22a/constants.py (commit in
// SOURCE). The comments are the ones uas_standards gives where it gives
// one; "Table 1" is the standard's table of data fields.

// Scope is an OAuth scope of the F3411 network Remote ID API.
type Scope string

// The two F3411 scopes (uas_standards Scope).
const (
	ScopeDisplayProvider Scope = "rid.display_provider"
	ScopeServiceProvider Scope = "rid.service_provider"
)

// Performance and display constants (uas_standards constants.py).
const (
	NetMinUasLocRefreshFrequencyHz             = 1
	NetMinUasLocRefreshPercentage              = 20
	NetMaxDisplayAreaDiagonalKm                = 7
	NetSpDataResponseTime95thPercentileSeconds = 1
	NetSpDataResponseTime99thPercentileSeconds = 3
	NetMaxNearRealTimeDataPeriodSeconds        = 60
	NetDpMaxDataRetentionPeriodSeconds         = 86400
	NetDpInitResponse95thPercentileSeconds     = 6
	NetDpInitResponse99thPercentileSeconds     = 18
	NetDpDataResponse95thPercentileSeconds     = 1
	NetDpDataResponse99thPercentileSeconds     = 3
	NetMinSessionLengthSeconds                 = 5
	NetDpDetailsResponse95thPercentileSeconds  = 2
	NetDpDetailsResponse99thPercentileSeconds  = 6
	NetDetailsMaxDisplayAreaDiagonalKm         = 2
	NetMinClusterSizePercent                   = 15
	NetMinObfuscationDistanceM                 = 300
	NetDSSMaxSubscriptionPerArea               = 10
	NetDSSMaxSubscriptionDurationHours         = 24
)

// Data field constants (uas_standards constants.py, Table 1).
const (
	// MinPositionResolution is the minimum resolution of both latitude
	// and longitude values, in degrees.
	MinPositionResolution = 0.0000001
	// MaxSpeed is the maximum value for ground speed, in metres per
	// second; a reported 254.25 means "254.25 or more".
	MaxSpeed = 254.25
	// SpecialSpeed is the ground speed meaning Invalid, No Value or
	// Unknown.
	SpecialSpeed = 255
	// MinSpeedResolution is the minimum resolution of ground speed, in
	// metres per second.
	MinSpeedResolution = 0.25
	// MaxAbsVerticalSpeed is the maximum absolute vertical speed relative
	// to the WGS-84 datum, in metres per second.
	MaxAbsVerticalSpeed = 62
	// SpecialVerticalSpeed is the vertical speed meaning Invalid, No Value
	// or Unknown.
	SpecialVerticalSpeed = 63
	// MinHeightResolution is the minimum resolution of height, in metres.
	MinHeightResolution = 1
	// MinOperatorAltitudeResolution is the minimum resolution of operator
	// altitude, in metres.
	MinOperatorAltitudeResolution = 1
	// SpecialHeight is the height (and altitude) meaning Invalid, No Value
	// or Unknown, in metres.
	SpecialHeight = -1000
	// MinTrackDirection is the minimum track direction (inclusive), in
	// degrees.
	MinTrackDirection = 0
	// MaxTrackDirection is the maximum track direction (exclusive), in
	// degrees; SpecialTrackDirection is also allowed.
	MaxTrackDirection = 360
	// SpecialTrackDirection is the track direction meaning Invalid, No
	// Value or Unknown.
	SpecialTrackDirection = 361
	// MinTrackDirectionResolution is the minimum resolution of track
	// direction, in degrees.
	MinTrackDirectionResolution = 1
	// MinTimestampResolution is the minimum resolution of a timestamp, in
	// seconds.
	MinTimestampResolution = 0.1
	// MinTimestampAccuracyResolution is the minimum resolution of the
	// timestamp accuracy, in seconds.
	MinTimestampAccuracyResolution = 0.1
)
