package conf

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/pmp4"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/logger"
)

var (
	rePathName              = regexp.MustCompile(`^[0-9a-zA-Z_\-/\.]+$`)
	reSourcePlaceholderPort = regexp.MustCompile(`:\$G[0-9]+`)
	reSourcePlaceholder     = regexp.MustCompile(`\$G[0-9]+`)
)

// IsValidPathName checks whether the path name is valid.
func IsValidPathName(name string) error {
	if name == "" {
		return fmt.Errorf("cannot be empty")
	}

	if name[0] == '/' {
		return fmt.Errorf("can't begin with a slash")
	}

	if name[len(name)-1] == '/' {
		return fmt.Errorf("can't end with a slash")
	}

	if !rePathName.MatchString(name) {
		return fmt.Errorf("can contain only alphanumeric characters, underscore, dot, minus, slash")
	}

	// prevent directory traversal attacks
	for segment := range strings.SplitSeq(name, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("can't contain dot path segments")
		}
	}

	return nil
}

func checkSRTPassphrase(passphrase string) error {
	switch {
	case len(passphrase) < 10 || len(passphrase) > 79:
		return fmt.Errorf("must be between 10 and 79 characters")

	default:
		return nil
	}
}

func checkRedirect(v string) error {
	if strings.HasPrefix(v, "/") {
		err := IsValidPathName(v[1:])
		if err != nil {
			return fmt.Errorf("'%s': %w", v, err)
		}
	} else {
		_, err := base.ParseURL(v)
		if err != nil {
			return fmt.Errorf("'%s' is not a valid RTSP URL", v)
		}
	}

	return nil
}

func validateURL(source string) (*url.URL, error) {
	s := reSourcePlaceholderPort.ReplaceAllString(source, ":1935")
	s = reSourcePlaceholder.ReplaceAllString(s, "abc")

	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("'%s' is not a valid URL", source)
	}

	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("'%s' is not a valid URL", source)
	}

	if u.User != nil {
		// An explicitly empty password ("user:@host") is valid and is required
		// by some legacy devices, whose password cannot be set (#395).
		// A missing password ("user@host") or a missing username (":pass@host")
		// is most probably a typo and is rejected.
		pass, passSet := u.User.Password()
		user := u.User.Username()
		switch {
		case user != "" && !passSet:
			return nil, fmt.Errorf("username was provided but password is missing; " +
				"if the password is intentionally empty, use 'user:@host'")
		case user == "" && pass != "":
			return nil, fmt.Errorf("password was provided but username is missing")
		}
	}

	return u, nil
}

func checkMP4MagicBytes(f io.ReadSeeker) error {
	magicBytes := make([]byte, 4)

	_, err := f.Seek(4, io.SeekStart)
	if err != nil {
		return err
	}

	_, err = io.ReadFull(f, magicBytes)
	if err != nil {
		return err
	}

	if !bytes.Equal(magicBytes, []byte("ftyp")) {
		return fmt.Errorf("file is not MP4, magic bytes are %v", magicBytes)
	}

	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	return nil
}

func checkAlwaysAvailableFile(fpath string) error {
	f, err := os.Open(fpath)
	if err != nil {
		return err
	}
	defer f.Close()

	err = checkMP4MagicBytes(f)
	if err != nil {
		return err
	}

	var presentation pmp4.Presentation
	err = presentation.Unmarshal(f)
	if err != nil {
		return err
	}

	if len(presentation.Tracks) == 0 {
		return fmt.Errorf("file does not contain any track")
	}

	for _, track := range presentation.Tracks {
		switch track.Codec.(type) {
		case *codecs.AV1, *codecs.VP9, *codecs.H265, *codecs.H264, *codecs.Opus, *codecs.MPEG4Audio, *codecs.LPCM:
		default:
			return fmt.Errorf("unsupported codec %T", track.Codec)
		}
	}

	return nil
}

// FindPathConf returns the configuration corresponding to the given path name.
func FindPathConf(pathConfs map[string]*Path, name string) (*Path, []string, error) {
	// static path configuration
	if pathConf, ok := pathConfs[name]; ok {
		return pathConf, nil, nil
	}

	// regexp path configuration

	err := IsValidPathName(name)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid path name: %w (%s)", err, name)
	}

	// gather and sort all regexp path configs
	var regexpPathConfs []*Path
	for _, pathConf := range pathConfs {
		if pathConf.Regexp != nil {
			regexpPathConfs = append(regexpPathConfs, pathConf)
		}
	}
	sort.Slice(regexpPathConfs, func(i, j int) bool {
		// keep all and all_others at the end
		if regexpPathConfs[i].Name == "all" || regexpPathConfs[i].Name == "all_others" {
			return false
		}
		if regexpPathConfs[j].Name == "all" || regexpPathConfs[j].Name == "all_others" {
			return true
		}
		return regexpPathConfs[i].Name < regexpPathConfs[j].Name
	})

	// check path against regexp path configs
	for _, pathConf := range regexpPathConfs {
		m := pathConf.Regexp.FindStringSubmatch(name)
		if m != nil {
			return pathConf, m, nil
		}
	}

	return nil, nil, fmt.Errorf("path '%s' is not configured", name)
}

// Path is a path configuration.
type Path struct {
	Regexp *regexp.Regexp `json:"-"`    // filled by Validate()
	Name   string         `json:"name"` // filled by Validate()

	// General
	Source                     string   `json:"source"`
	SourceFingerprint          string   `json:"sourceFingerprint"`
	SourceOnDemand             bool     `json:"sourceOnDemand"`
	SourceOnDemandStartTimeout Duration `json:"sourceOnDemandStartTimeout"`
	SourceOnDemandCloseAfter   Duration `json:"sourceOnDemandCloseAfter"`
	MaxReaders                 int      `json:"maxReaders"`
	SRTReadPassphrase          string   `json:"srtReadPassphrase"`
	Fallback                   *string  `json:"fallback,omitempty" deprecated:"true"`
	UseAbsoluteTimestamp       bool     `json:"useAbsoluteTimestamp"`

	// Always available
	AlwaysAvailable         bool                   `json:"alwaysAvailable"`
	AlwaysAvailableTracks   []AlwaysAvailableTrack `json:"alwaysAvailableTracks"`
	AlwaysAvailableFile     string                 `json:"alwaysAvailableFile"`
	AlwaysAvailableRecorded bool                   `json:"alwaysAvailableRecorded"`

	// Forward
	Forward Forward `json:"forward"`

	// Record
	Record                bool         `json:"record"`
	Playback              *bool        `json:"playback,omitempty" deprecated:"true"`
	RecordPath            string       `json:"recordPath"`
	RecordFormat          RecordFormat `json:"recordFormat"`
	RecordPartDuration    Duration     `json:"recordPartDuration"`
	RecordMaxPartSize     StringSize   `json:"recordMaxPartSize"`
	RecordSegmentDuration Duration     `json:"recordSegmentDuration"`
	RecordDeleteAfter     Duration     `json:"recordDeleteAfter"`
	// MTX-2 (hub/third_party/PATCHES.md): RI_g and RK, the two age1 X25519
	// recipients that fMP4 segments are encrypted to; empty records in clear
	// (Load turns an absent list into an empty one). They are kept as strings:
	// Clone copies settable fields only, and would drop the key bytes inside a
	// parsed recipient.
	RecordAgeRecipients []string `json:"recordAgeRecipients"`
	// MTX-2: whether an encrypted part smaller than one age chunk is always
	// padded to the chunk boundary (the per-camera setting O17, on by default).
	RecordAgePadLowBitrate bool `json:"recordAgePadLowBitrate"`

	// Authentication (deprecated)
	PublishUser *Credential `json:"publishUser,omitempty" deprecated:"true"`
	PublishPass *Credential `json:"publishPass,omitempty" deprecated:"true"`
	PublishIPs  *IPNetworks `json:"publishIPs,omitempty" deprecated:"true"`
	ReadUser    *Credential `json:"readUser,omitempty" deprecated:"true"`
	ReadPass    *Credential `json:"readPass,omitempty" deprecated:"true"`
	ReadIPs     *IPNetworks `json:"readIPs,omitempty" deprecated:"true"`

	// Publisher source
	OverridePublisher        bool   `json:"overridePublisher"`
	DisablePublisherOverride *bool  `json:"disablePublisherOverride,omitempty" deprecated:"true"`
	SRTPublishPassphrase     string `json:"srtPublishPassphrase"`
	RTSPDemuxMpegts          bool   `json:"rtspDemuxMpegts"`

	// RTSP source
	RTSPTransport          RTSPTransport  `json:"rtspTransport"`
	RTSPAnyPort            bool           `json:"rtspAnyPort"`
	SourceProtocol         *RTSPTransport `json:"sourceProtocol,omitempty" deprecated:"true"`
	SourceAnyPortEnable    *bool          `json:"sourceAnyPortEnable,omitempty" deprecated:"true"`
	RTSPRangeType          RTSPRangeType  `json:"rtspRangeType"`
	RTSPRangeStart         string         `json:"rtspRangeStart"`
	RTSPScale              string         `json:"rtspScale"`
	RTSPUDPReadBufferSize  *uint          `json:"rtspUDPReadBufferSize,omitempty" deprecated:"true"`
	RTSPUDPSourcePortRange []uint         `json:"rtspUDPSourcePortRange"`

	// MPEG-TS source
	MPEGTSUDPReadBufferSize *uint `json:"mpegtsUDPReadBufferSize,omitempty" deprecated:"true"`

	// RTP source
	RTPSDP               string `json:"rtpSDP"`
	RTPUDPReadBufferSize *uint  `json:"rtpUDPReadBufferSize,omitempty" deprecated:"true"`

	// MoQ source
	MoQTransport MoQTransport `json:"moqTransport"`

	// WHEP source
	WHEPBearerToken        string   `json:"whepBearerToken"`
	WHEPSTUNGatherTimeout  Duration `json:"whepSTUNGatherTimeout"`
	WHEPHandshakeTimeout   Duration `json:"whepHandshakeTimeout"`
	WHEPTrackGatherTimeout Duration `json:"whepTrackGatherTimeout"`

	// Redirect source
	SourceRedirect string `json:"sourceRedirect"`

	// Raspberry Pi Camera source
	RPICameraCamID                 uint      `json:"rpiCameraCamID"`
	RPICameraSecondary             bool      `json:"rpiCameraSecondary"`
	RPICameraWidth                 uint      `json:"rpiCameraWidth"`
	RPICameraHeight                uint      `json:"rpiCameraHeight"`
	RPICameraHFlip                 bool      `json:"rpiCameraHFlip"`
	RPICameraVFlip                 bool      `json:"rpiCameraVFlip"`
	RPICameraBrightness            float64   `json:"rpiCameraBrightness"`
	RPICameraContrast              float64   `json:"rpiCameraContrast"`
	RPICameraSaturation            float64   `json:"rpiCameraSaturation"`
	RPICameraSharpness             float64   `json:"rpiCameraSharpness"`
	RPICameraExposure              string    `json:"rpiCameraExposure"`
	RPICameraAWB                   string    `json:"rpiCameraAWB"`
	RPICameraAWBGains              []float64 `json:"rpiCameraAWBGains"`
	RPICameraDenoise               string    `json:"rpiCameraDenoise"`
	RPICameraShutter               uint      `json:"rpiCameraShutter"`
	RPICameraMetering              string    `json:"rpiCameraMetering"`
	RPICameraGain                  float64   `json:"rpiCameraGain"`
	RPICameraEV                    float64   `json:"rpiCameraEV"`
	RPICameraROI                   string    `json:"rpiCameraROI"`
	RPICameraHDR                   bool      `json:"rpiCameraHDR"`
	RPICameraTuningFile            string    `json:"rpiCameraTuningFile"`
	RPICameraMode                  string    `json:"rpiCameraMode"`
	RPICameraFPS                   float64   `json:"rpiCameraFPS"`
	RPICameraAfMode                string    `json:"rpiCameraAfMode"`
	RPICameraAfRange               string    `json:"rpiCameraAfRange"`
	RPICameraAfSpeed               string    `json:"rpiCameraAfSpeed"`
	RPICameraLensPosition          float64   `json:"rpiCameraLensPosition"`
	RPICameraAfWindow              string    `json:"rpiCameraAfWindow"`
	RPICameraFlickerPeriod         uint      `json:"rpiCameraFlickerPeriod"`
	RPICameraTextOverlayEnable     bool      `json:"rpiCameraTextOverlayEnable"`
	RPICameraTextOverlay           string    `json:"rpiCameraTextOverlay"`
	RPICameraCodec                 string    `json:"rpiCameraCodec"`
	RPICameraIDRPeriod             uint      `json:"rpiCameraIDRPeriod"`
	RPICameraBitrate               uint      `json:"rpiCameraBitrate"`
	RPICameraProfile               *string   `json:"rpiCameraProfile,omitempty" deprecated:"true"`
	RPICameraLevel                 *string   `json:"rpiCameraLevel,omitempty" deprecated:"true"`
	RPICameraHardwareH264Profile   *string   `json:"rpiCameraHardwareH264Profile,omitempty" deprecated:"true"`
	RPICameraHardwareH264Level     *string   `json:"rpiCameraHardwareH264Level,omitempty" deprecated:"true"`
	RPICameraSoftwareH264Profile   *string   `json:"rpiCameraSoftwareH264Profile,omitempty" deprecated:"true"`
	RPICameraSoftwareH264Level     *string   `json:"rpiCameraSoftwareH264Level,omitempty" deprecated:"true"`
	RPICameraH264Profile           string    `json:"rpiCameraH264Profile"`
	RPICameraH264Level             string    `json:"rpiCameraH264Level"`
	RPICameraJPEGQuality           *uint     `json:"rpiCameraJPEGQuality,omitempty" deprecated:"true"`
	RPICameraMJPEGQuality          uint      `json:"rpiCameraMJPEGQuality"`
	RPICameraPrimaryName           string    `json:"-"` // filled by Validate()
	RPICameraSecondaryCodec        string    `json:"-"` // filled by Validate()
	RPICameraSecondaryWidth        uint      `json:"-"` // filled by Validate()
	RPICameraSecondaryHeight       uint      `json:"-"` // filled by Validate()
	RPICameraSecondaryFPS          float64   `json:"-"` // filled by Validate()
	RPICameraSecondaryIDRPeriod    uint      `json:"-"` // filled by Validate()
	RPICameraSecondaryBitrate      uint      `json:"-"` // filled by Validate()
	RPICameraSecondaryH264Profile  string    `json:"-"` // filled by Validate()
	RPICameraSecondaryH264Level    string    `json:"-"` // filled by Validate()
	RPICameraSecondaryMJPEGQuality uint      `json:"-"` // filled by Validate()

	// Hooks
	RunOnInit                  string   `json:"runOnInit"`
	RunOnInitRestart           bool     `json:"runOnInitRestart"`
	RunOnDemand                string   `json:"runOnDemand"`
	RunOnDemandRestart         bool     `json:"runOnDemandRestart"`
	RunOnDemandStartTimeout    Duration `json:"runOnDemandStartTimeout"`
	RunOnDemandCloseAfter      Duration `json:"runOnDemandCloseAfter"`
	RunOnUnDemand              string   `json:"runOnUnDemand"`
	RunOnAvailable             string   `json:"runOnAvailable"`
	RunOnAvailableRestart      bool     `json:"runOnAvailableRestart"`
	RunOnUnavailable           string   `json:"runOnUnavailable"`
	RunOnReady                 *string  `json:"runOnReady,omitempty" deprecated:"true"`
	RunOnReadyRestart          *bool    `json:"runOnReadyRestart,omitempty" deprecated:"true"`
	RunOnNotReady              *string  `json:"runOnNotReady,omitempty" deprecated:"true"`
	RunOnOnline                string   `json:"runOnOnline"`
	RunOnOnlineRestart         bool     `json:"runOnOnlineRestart"`
	RunOnOffline               string   `json:"runOnOffline"`
	RunOnRead                  string   `json:"runOnRead"`
	RunOnReadRestart           bool     `json:"runOnReadRestart"`
	RunOnUnread                string   `json:"runOnUnread"`
	RunOnRecordSegmentCreate   string   `json:"runOnRecordSegmentCreate"`
	RunOnRecordSegmentComplete string   `json:"runOnRecordSegmentComplete"`
}

func (pconf *Path) setDefaults() {
	// General
	pconf.Source = "publisher"
	pconf.SourceOnDemandStartTimeout = 10 * Duration(time.Second)
	pconf.SourceOnDemandCloseAfter = 10 * Duration(time.Second)

	// Always available
	pconf.AlwaysAvailableRecorded = true

	// Record
	pconf.RecordPath = "./recordings/%path/%Y-%m-%d_%H-%M-%S-%f"
	pconf.RecordFormat = RecordFormatFMP4
	pconf.RecordPartDuration = Duration(1 * time.Second)
	pconf.RecordMaxPartSize = 50 * 1024 * 1024
	pconf.RecordSegmentDuration = 3600 * Duration(time.Second)
	pconf.RecordDeleteAfter = 24 * 3600 * Duration(time.Second)
	pconf.RecordAgePadLowBitrate = true // MTX-2: O17's default

	// Publisher source
	pconf.OverridePublisher = true

	// RTSP source
	pconf.RTSPUDPSourcePortRange = []uint{32768, 60999}

	// MoQ source
	pconf.MoQTransport = MoQTransportQUIC

	// WHEP source
	pconf.WHEPSTUNGatherTimeout = Duration(5 * time.Second)
	pconf.WHEPHandshakeTimeout = Duration(10 * time.Second)
	pconf.WHEPTrackGatherTimeout = Duration(2 * time.Second)

	// Raspberry Pi Camera source
	pconf.RPICameraWidth = 1920
	pconf.RPICameraHeight = 1080
	pconf.RPICameraContrast = 1
	pconf.RPICameraSaturation = 1
	pconf.RPICameraSharpness = 1
	pconf.RPICameraExposure = "normal"
	pconf.RPICameraAWB = "auto"
	pconf.RPICameraAWBGains = []float64{0, 0}
	pconf.RPICameraDenoise = "off"
	pconf.RPICameraMetering = "centre"
	pconf.RPICameraFPS = 30
	pconf.RPICameraAfMode = "continuous"
	pconf.RPICameraAfRange = "normal"
	pconf.RPICameraAfSpeed = "normal"
	pconf.RPICameraTextOverlay = "%Y-%m-%d %H:%M:%S - MediaMTX"
	pconf.RPICameraCodec = "auto"
	pconf.RPICameraIDRPeriod = 60
	pconf.RPICameraBitrate = 5000000
	pconf.RPICameraH264Profile = "auto"
	pconf.RPICameraH264Level = "4.1"
	pconf.RPICameraMJPEGQuality = 60

	// Hooks
	pconf.RunOnDemandStartTimeout = 10 * Duration(time.Second)
	pconf.RunOnDemandCloseAfter = 10 * Duration(time.Second)
}

func newPath(defaults *Path, partial *OptionalPath) *Path {
	pconf := &Path{}
	copyStructFields(pconf, defaults)
	copyStructFields(pconf, partial.Values)
	return pconf
}

// Clone clones the configuration.
func (pconf Path) Clone() *Path {
	cloned := deepClone(reflect.ValueOf(pconf)).Interface().(Path)
	return &cloned
}

func (pconf *Path) validate(
	conf *Conf,
	name string,
	deprecatedCredentialsMode bool,
	l logger.Writer,
) error {
	pconf.Name = name

	switch {
	case name == "all_others", name == "all":
		pconf.Regexp = regexp.MustCompile("^.*$")

	case name == "" || name[0] != '~': // normal path
		err := IsValidPathName(name)
		if err != nil {
			return fmt.Errorf("invalid path name '%s': %w", name, err)
		}

	default: // regular expression-based path
		regexp, err := regexp.Compile(name[1:])
		if err != nil {
			return fmt.Errorf("invalid regular expression: %s", name[1:])
		}
		pconf.Regexp = regexp
	}

	// common configuration errors

	if pconf.SRTPublishPassphrase != "" && pconf.Source != "publisher" {
		return fmt.Errorf("'srtPublishPassphase' can only be used when source is 'publisher'")
	}

	if pconf.Source != "redirect" && pconf.SourceRedirect != "" {
		return fmt.Errorf("'sourceRedirect' is useless when source is not 'redirect'")
	}

	// General

	switch {
	case pconf.Source == "publisher":
		if pconf.DisablePublisherOverride != nil {
			l.Log(logger.Warn, "parameter 'disablePublisherOverride' is deprecated "+
				"and has been replaced with 'overridePublisher'")
			pconf.OverridePublisher = !*pconf.DisablePublisherOverride
		}

		if pconf.SRTPublishPassphrase != "" {
			err := checkSRTPassphrase(pconf.SRTPublishPassphrase)
			if err != nil {
				return fmt.Errorf("invalid 'srtPublishPassphrase': %w", err)
			}
		}

	case strings.HasPrefix(pconf.Source, "rtsp://") ||
		strings.HasPrefix(pconf.Source, "rtsps://") ||
		strings.HasPrefix(pconf.Source, "rtsp+http://") ||
		strings.HasPrefix(pconf.Source, "rtsps+http://") ||
		strings.HasPrefix(pconf.Source, "rtsp+ws://") ||
		strings.HasPrefix(pconf.Source, "rtsps+ws://"):
		_, err := validateURL(pconf.Source)
		if err != nil {
			return err
		}

		if pconf.SourceProtocol != nil {
			l.Log(logger.Warn, "parameter 'sourceProtocol' is deprecated and has been replaced with 'rtspTransport'")
			pconf.RTSPTransport = *pconf.SourceProtocol
		}

		if pconf.SourceAnyPortEnable != nil {
			l.Log(logger.Warn, "parameter 'sourceAnyPortEnable' is deprecated and has been replaced with 'rtspAnyPort'")
			pconf.RTSPAnyPort = *pconf.SourceAnyPortEnable
		}

	case strings.HasPrefix(pconf.Source, "rtmp://") ||
		strings.HasPrefix(pconf.Source, "rtmps://"):
		_, err := validateURL(pconf.Source)
		if err != nil {
			return err
		}

	case strings.HasPrefix(pconf.Source, "http://") ||
		strings.HasPrefix(pconf.Source, "https://"):
		_, err := validateURL(pconf.Source)
		if err != nil {
			return err
		}

	case strings.HasPrefix(pconf.Source, "udp://"):
		u, err := validateURL(pconf.Source)
		if err != nil {
			return err
		}

		_, _, err = net.SplitHostPort(u.Host)
		if err != nil {
			return fmt.Errorf("'%s' is missing the port", pconf.Source)
		}

	case strings.HasPrefix(pconf.Source, "udp+mpegts://"):
		u, err := validateURL(pconf.Source)
		if err != nil {
			return err
		}

		_, _, err = net.SplitHostPort(u.Host)
		if err != nil {
			return fmt.Errorf("'%s' is missing the port", pconf.Source)
		}

	case strings.HasPrefix(pconf.Source, "unix+mpegts://"):

	case strings.HasPrefix(pconf.Source, "udp+rtp://"):
		u, err := validateURL(pconf.Source)
		if err != nil {
			return err
		}

		_, _, err = net.SplitHostPort(u.Host)
		if err != nil {
			return fmt.Errorf("'%s' is missing the port", pconf.Source)
		}

		if pconf.RTPSDP == "" {
			return fmt.Errorf("`rtpSDP` was not provided")
		}

	case strings.HasPrefix(pconf.Source, "unix+rtp://"):
		l.Log(logger.Warn, "source 'unix+rtp' is deprecated due to intrinsic instability, use 'udp+rtp' instead")

		if pconf.RTPSDP == "" {
			return fmt.Errorf("`rtpSDP` was not provided")
		}

	case strings.HasPrefix(pconf.Source, "srt://"):
		_, err := validateURL(pconf.Source)
		if err != nil {
			return err
		}

	case strings.HasPrefix(pconf.Source, "moqt://"):
		_, err := validateURL(pconf.Source)
		if err != nil {
			return err
		}

	case strings.HasPrefix(pconf.Source, "whep://") ||
		strings.HasPrefix(pconf.Source, "wheps://"):
		_, err := validateURL(pconf.Source)
		if err != nil {
			return err
		}

	case pconf.Source == "redirect":
		if pconf.SourceRedirect == "" {
			return fmt.Errorf("source redirect must be filled")
		}

		err := checkRedirect(pconf.SourceRedirect)
		if err != nil {
			return err
		}

	case pconf.Source == "rpiCamera":
		return fmt.Errorf("source 'rpiCamera' is not supported: this build has no Raspberry Pi camera source")

	default:
		return fmt.Errorf("invalid source: '%s'", pconf.Source)
	}

	if pconf.SourceOnDemand {
		if pconf.Source == "publisher" {
			return fmt.Errorf("'sourceOnDemand' is useless when source is 'publisher'")
		}
	} else {
		if pconf.Source != "publisher" && pconf.Source != "redirect" && pconf.Regexp != nil {
			return fmt.Errorf("a path with a regular expression (or path 'all_others') and a static source" +
				" must have 'sourceOnDemand' set to true")
		}
	}

	if pconf.SRTReadPassphrase != "" {
		err := checkSRTPassphrase(pconf.SRTReadPassphrase)
		if err != nil {
			return fmt.Errorf("invalid 'readRTPassphrase': %w", err)
		}
	}

	err := pconf.Forward.Validate()
	if err != nil {
		return fmt.Errorf("invalid 'forward': %w", err)
	}

	if pconf.Fallback != nil {
		l.Log(logger.Warn, "the 'fallback' feature is deprecated, use 'alwaysAvailable' instead")
		err = checkRedirect(*pconf.Fallback)
		if err != nil {
			return err
		}
	}

	// Always available

	if pconf.AlwaysAvailable {
		if pconf.Regexp != nil {
			return fmt.Errorf("'alwaysAvailable' cannot be used in a path with a regular expression (or path 'all_others')")
		}

		if pconf.SourceOnDemand {
			return fmt.Errorf("'sourceOnDemand' is not compatible with 'alwaysAvailable'")
		}

		if pconf.RunOnDemand != "" || pconf.RunOnUnDemand != "" {
			return fmt.Errorf("'runOnDemand' and 'runOnUnDemand' cannot be used with 'alwaysAvailable'")
		}

		if pconf.AlwaysAvailableFile != "" {
			if len(pconf.AlwaysAvailableTracks) != 0 {
				return fmt.Errorf("'alwaysAvailableFile' and 'alwaysAvailableTracks' cannot be used together")
			}

			err = checkAlwaysAvailableFile(pconf.AlwaysAvailableFile)
			if err != nil {
				return fmt.Errorf("invalid 'alwaysAvailableFile': %w", err)
			}
		} else if len(pconf.AlwaysAvailableTracks) == 0 {
			return fmt.Errorf("'alwaysAvailableTracks' must contain at least one track")
		}

		if pconf.UseAbsoluteTimestamp {
			return fmt.Errorf("'useAbsoluteTimestamp' cannot be used with 'alwaysAvailable'")
		}
	}

	// Record

	if pconf.Playback != nil {
		l.Log(logger.Warn, "parameter 'playback' is deprecated and has no effect")
	}

	if !strings.Contains(pconf.RecordPath, "%path") {
		return fmt.Errorf("'recordPath' must contain %%path")
	}

	if !strings.Contains(pconf.RecordPath, "%s") &&
		(!strings.Contains(pconf.RecordPath, "%Y") ||
			!strings.Contains(pconf.RecordPath, "%m") ||
			!strings.Contains(pconf.RecordPath, "%d") ||
			!strings.Contains(pconf.RecordPath, "%H") ||
			!strings.Contains(pconf.RecordPath, "%M") ||
			!strings.Contains(pconf.RecordPath, "%S")) {
		return fmt.Errorf("'recordPath' must contain either %%s or %%Y %%m %%d %%H %%M %%S")
	}

	if conf.Playback && !strings.Contains(pconf.RecordPath, "%f") {
		return fmt.Errorf("'recordPath' must contain %%f")
	}

	if pconf.RecordSegmentDuration > Duration(24*time.Hour) { // avoid overflowing DurationV0 of mvhd
		return fmt.Errorf("maximum segment duration is 1 day")
	}

	if pconf.RecordDeleteAfter != 0 && pconf.RecordDeleteAfter < pconf.RecordSegmentDuration {
		return fmt.Errorf("'recordDeleteAfter' cannot be lower than 'recordSegmentDuration'")
	}

	// MTX-2
	if _, err := pconf.RecordAgeScope(); err != nil {
		return err
	}
	if len(pconf.RecordAgeRecipients) != 0 && pconf.RecordFormat != RecordFormatFMP4 {
		return fmt.Errorf("'recordAgeRecipients' needs 'recordFormat: fmp4': MPEG-TS segments are not encrypted")
	}

	// Authentication (deprecated)

	if deprecatedCredentialsMode {
		func() {
			var user Credential = "any"
			if pconf.PublishUser != nil && *pconf.PublishUser != "" {
				user = *pconf.PublishUser
			}

			var pass Credential
			if pconf.PublishPass != nil && *pconf.PublishPass != "" {
				pass = *pconf.PublishPass
			}

			ips := IPNetworks{mustParseCIDR("0.0.0.0/0")}
			if pconf.PublishIPs != nil && len(*pconf.PublishIPs) != 0 {
				ips = *pconf.PublishIPs
			}

			pathName := name
			if name == "all_others" || name == "all" {
				pathName = "~^.*$"
			}

			conf.AuthInternalUsers = append(conf.AuthInternalUsers, AuthInternalUser{
				User: user,
				Pass: pass,
				IPs:  ips,
				Permissions: []AuthInternalUserPermission{{
					Action: AuthActionPublish,
					Path:   pathName,
				}},
			})
		}()

		func() {
			var user Credential = "any"
			if pconf.ReadUser != nil && *pconf.ReadUser != "" {
				user = *pconf.ReadUser
			}

			var pass Credential
			if pconf.ReadPass != nil && *pconf.ReadPass != "" {
				pass = *pconf.ReadPass
			}

			ips := IPNetworks{mustParseCIDR("0.0.0.0/0")}
			if pconf.ReadIPs != nil && len(*pconf.ReadIPs) != 0 {
				ips = *pconf.ReadIPs
			}

			pathName := name
			if name == "all_others" || name == "all" {
				pathName = "~^.*$"
			}

			conf.AuthInternalUsers = append(conf.AuthInternalUsers, AuthInternalUser{
				User: user,
				Pass: pass,
				IPs:  ips,
				Permissions: []AuthInternalUserPermission{{
					Action: AuthActionRead,
					Path:   pathName,
				}},
			})
		}()
	}

	// Hooks

	if pconf.RunOnInit != "" && pconf.Regexp != nil {
		return fmt.Errorf("a path with a regular expression (or path 'all_others')" +
			" does not support option 'runOnInit'; use another path")
	}

	if pconf.RunOnReady != nil {
		l.Log(logger.Warn, "parameter 'runOnReady' is deprecated and has been replaced with 'runOnAvailable'")
		pconf.RunOnAvailable = *pconf.RunOnReady
	}

	if pconf.RunOnReadyRestart != nil {
		l.Log(logger.Warn, "parameter 'runOnReadyRestart' is deprecated and has been replaced with 'runOnAvailableRestart'")
		pconf.RunOnAvailableRestart = *pconf.RunOnReadyRestart
	}

	if pconf.RunOnNotReady != nil {
		l.Log(logger.Warn, "parameter 'runOnNotReady' is deprecated and has been replaced with 'runOnUnavailable'")
		pconf.RunOnUnavailable = *pconf.RunOnNotReady
	}

	if (pconf.RunOnDemand != "" || pconf.RunOnUnDemand != "") && pconf.Source != "publisher" {
		return fmt.Errorf("'runOnDemand' and 'runOnUnDemand' can be used only when source is 'publisher'")
	}

	return nil
}

// RecordAgeScope returns the recipients that the path's fMP4 segments are
// encrypted to (MTX-2), or nil when the list is empty and the path records in
// clear. A list must hold exactly RI_g and RK, two different age1 X25519
// recipients. Its errors never quote it: an entry may be a secret key pasted
// in the wrong place.
func (pconf *Path) RecordAgeScope() (*crypto.KeyScope, error) {
	if len(pconf.RecordAgeRecipients) == 0 {
		return nil, nil
	}
	if len(pconf.RecordAgeRecipients) != 2 {
		return nil, fmt.Errorf("'recordAgeRecipients' must hold exactly two age1 X25519 recipients, RI_g and RK, not %d",
			len(pconf.RecordAgeRecipients))
	}
	scope, err := crypto.ParseKeyScope(pconf.RecordAgeRecipients[0], pconf.RecordAgeRecipients[1])
	if err != nil {
		return nil, fmt.Errorf("invalid 'recordAgeRecipients': %w", err)
	}
	return &scope, nil
}

// Equal checks whether two Paths are equal.
func (pconf *Path) Equal(other *Path) bool {
	return reflect.DeepEqual(pconf, other)
}

// HasStaticSource checks whether the path has a static source.
func (pconf Path) HasStaticSource() bool {
	return pconf.Source != "publisher" && pconf.Source != "redirect"
}

// HasOnDemandStaticSource checks whether the path has a on demand static source.
func (pconf Path) HasOnDemandStaticSource() bool {
	return pconf.HasStaticSource() && pconf.SourceOnDemand
}

// HasOnDemandPublisher checks whether the path has a on-demand publisher.
func (pconf Path) HasOnDemandPublisher() bool {
	return pconf.RunOnDemand != ""
}
