package f3548

// Constants of ASTM F3548-21, copied with their names and values from
// uas_standards src/uas_standards/astm/f3548/v21/constants.py (commit in
// SOURCE).

// Scope is an OAuth scope of the F3548 API.
type Scope string

// The five F3548 scopes (uas_standards Scope).
const (
	ScopeStrategicCoordination                        Scope = "utm.strategic_coordination"
	ScopeConstraintManagement                         Scope = "utm.constraint_management"
	ScopeConstraintProcessing                         Scope = "utm.constraint_processing"
	ScopeConformanceMonitoringForSituationalAwareness Scope = "utm.conformance_monitoring_sa"
	ScopeAvailabilityArbitration                      Scope = "utm.availability_arbitration"
)

// Timing, size and performance constants (uas_standards constants.py).
const (
	AggConfMonEvaluationFlightHours                  = 10
	AggConfMonEvaluationPeriodDays                   = 7
	ConflictingOIMaxUserNotificationTimeSeconds      = 5
	ConflictingOIMaxUSSNotificationTimeSeconds       = 1
	CstrPublishedNotificationLatencySeconds          = 5
	CstrMaxAreaKm2                                   = 10000
	CstrMaxDeletionSeconds                           = 5
	CstrMaxDurationHours                             = 24
	CstrMaxPlanningHorizonDays                       = 56
	CstrMaxTimeSendDetailsSeconds                    = 5
	CstrMaxVertices                                  = 1000
	CstrMinEffectiveTimeBufferMinutes                = 10
	DSSMaxSubscriptionDurationHours                  = 24
	ExternalDataMaxRetentionTimeHours                = 24
	IntersectingConstraintUserNotificationMaxSeconds = 5
	IntersectionMinimumPrecisionCm                   = 1
	MaxAggConfMonAnalysisLatencyHours                = 24
	MaxNonPerformanceNotificationLatencyHours        = 6
	MaxRecoverableTimeInNonconformingStateSeconds    = 60
	MaxRespondToSubscriptionNotificationSeconds      = 5
	MaxRespondToOIDetailsRequestSeconds              = 1
	OiMaxCancelTimeSeconds                           = 5
	OiMaxDurationPerExcursionSeconds                 = 10
	OiMaxExcursionsPerFlightHour                     = 18
	OiMaxPlanHorizonDays                             = 30
	OiMaxUpdateRestoreConfSeconds                    = 5
	OiMaxUpdateTimeContingentSeconds                 = 5
	OiMaxUpdateTimeNonconfSeconds                    = 5
	OiMaxVertices                                    = 10000
	OiMinConformancePercent                          = 95
	PosInfoRequestMaxResponseTimeSeconds             = 5
	TimeSyncMaxDifferentialSeconds                   = 5
	TimeSyncMinPercentage                            = 99
	TransitionToEndedMaxTimeSeconds                  = 5
	UnableToDeliverConstraintDetailsSeconds          = 5
	UserOiStateChangeNotificationMaxSeconds          = 5
	UssFunctionFailureNotificationMaxSeconds         = 5
	UssOiChangeNotificationMaxSeconds                = 5
)
