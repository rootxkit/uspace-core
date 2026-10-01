package cell

import (
	"errors"
	"math"
	"math/rand/v2"
	"runtime"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
)

func ll(latDeg, lonDeg float64) core.LatLon { return core.LatLon{LatDeg: latDeg, LonDeg: lonDeg} }

func mustOf(t *testing.T, p core.LatLon, l Level) ID {
	t.Helper()
	c, err := Of(p, l)
	if err != nil {
		t.Fatalf("Of(%v, %d): %v", p, l, err)
	}
	return c
}

func wantFieldError(t *testing.T, err error, field string) {
	t.Helper()
	var fe *core.FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("error %v is not a *core.FieldError", err)
	}
	if fe.Field != field {
		t.Fatalf("FieldError.Field = %q, want %q (%v)", fe.Field, field, err)
	}
}

// inCell reports whether p is in the cell's half-open box, with the
// last row closed at +90.
func inCell(c ID, p core.LatLon) bool {
	b := c.BBox()
	lon := p.LonDeg
	if lon == 180 {
		lon = -180
	}
	latOK := p.LatDeg >= b.MinLat && (p.LatDeg < b.MaxLat || (b.MaxLat == 90 && p.LatDeg == 90))
	return latOK && lon >= b.MinLon && lon < b.MaxLon
}

// TestTable pins the WP-15 table, each value checked by hand from the
// formula: Tbilisi (41.7151 + 90) * 10 = 1317.151 and (44.8271 + 180) *
// 10 = 2248.271; 90 is clamped to the last row; 179.95 is in column
// floor(3599.5) = 3599; 180 is -180, column 0.
func TestTable(t *testing.T) {
	cases := []struct {
		p      core.LatLon
		c5, c3 string
	}{
		{ll(41.7151, 44.8271), "c5:1317:2248", "c3:131:224"},
		{ll(0, 0), "c5:900:1800", "c3:90:180"},
		{ll(-90, -180), "c5:0:0", "c3:0:0"},
		{ll(90, 179.95), "c5:1799:3599", "c3:179:359"},
		{ll(90, 180), "c5:1799:0", "c3:179:0"},
	}
	for _, tc := range cases {
		c5 := mustOf(t, tc.p, Level5)
		c3 := mustOf(t, tc.p, Level3)
		if c5.String() != tc.c5 || c3.String() != tc.c3 {
			t.Errorf("%v: got %s %s, want %s %s", tc.p, c5, c3, tc.c5, tc.c3)
		}
		if c5.Parent() != c3 {
			t.Errorf("%v: Parent(%s) = %s, want %s", tc.p, c5, c5.Parent(), c3)
		}
		if !inCell(c5, tc.p) || !inCell(c3, tc.p) {
			t.Errorf("%v not inside its cells %v %v", tc.p, c5.BBox(), c3.BBox())
		}
	}
}

func TestLevel(t *testing.T) {
	if Level5.StepDeg() != 0.1 || Level3.StepDeg() != 1 {
		t.Fatalf("StepDeg = %v, %v", Level5.StepDeg(), Level3.StepDeg())
	}
	if !Level5.Valid() || !Level3.Valid() {
		t.Fatal("Level5 and Level3 must be valid")
	}
	for _, l := range []Level{0, 1, 4, 6, -5} {
		if l.Valid() || l.StepDeg() != 0 {
			t.Errorf("Level(%d) must be invalid with StepDeg 0", l)
		}
	}
}

func TestStringParseRoundTrip(t *testing.T) {
	check := func(c ID) {
		t.Helper()
		s := c.String()
		got, err := Parse(s)
		if err != nil || got != c {
			t.Fatalf("Parse(%q) = %v, %v; want %v", s, got, err, c)
		}
	}
	// Every c3 cell of the globe, and every c5 of its first and last
	// rows and of the Tbilisi c3.
	for i := range 180 {
		for j := range 360 {
			check(ID{Level: Level3, LatIdx: i, LonIdx: j})
		}
	}
	for _, i := range []int{0, 1799} {
		for j := range 3600 {
			check(ID{Level: Level5, LatIdx: i, LonIdx: j})
		}
	}
	for _, c := range (ID{Level: Level3, LatIdx: 131, LonIdx: 224}).Children() {
		check(c)
	}
	rng := rand.New(rand.NewPCG(1, 15))
	for range 10_000 {
		p := ll(rng.Float64()*180-90, rng.Float64()*360-180)
		check(mustOf(t, p, Level5))
		check(mustOf(t, p, Level3))
	}
}

// TestParseRefusals pairs every refused spelling with the accepted one
// it differs from (E-01).
func TestParseRefusals(t *testing.T) {
	cases := []struct{ refused, accepted string }{
		{"c5:041:5", "c5:41:5"},
		{"c5:41:05", "c5:41:5"},
		{"c5:00:0", "c5:0:0"},
		{"C5:1:2", "c5:1:2"},
		{"c5:1:2:3", "c5:1:2"},
		{"h3:1:2", "c3:1:2"},
		{"c4:1:2", "c3:1:2"},
		{"c5:+1:2", "c5:1:2"},
		{"c5:-1:2", "c5:1:2"},
		{"c5:1:-2", "c5:1:2"},
		{"c5: 1:2", "c5:1:2"},
		{"c5:1:2 ", "c5:1:2"},
		{" c5:1:2", "c5:1:2"},
		{"c5:1::2", "c5:1:2"},
		{"c5::2", "c5:0:2"},
		{"c5:1:", "c5:1:0"},
		{"c5:1", "c5:1:0"},
		{"c5:", "c5:0:0"},
		{"c5", "c5:0:0"},
		{"", "c3:0:0"},
		{"c5:1800:0", "c5:1799:0"},
		{"c5:0:3600", "c5:0:3599"},
		{"c3:180:0", "c3:179:0"},
		{"c3:0:360", "c3:0:359"},
		{"c5:1799:35990", "c5:1799:3599"},
		{"c5:99999999999999999999:1", "c5:999:1"},
		{"c5:1a:2", "c5:1:2"},
		{"c5:1:2\x00", "c5:1:2"},
		{"c5：1:2", "c5:1:2"},
	}
	for _, tc := range cases {
		if c, err := Parse(tc.accepted); err != nil || !c.Valid() || c.String() != tc.accepted {
			t.Errorf("Parse(%q) = %v, %v; want it accepted", tc.accepted, c, err)
		}
		c, err := Parse(tc.refused)
		if err == nil {
			t.Errorf("Parse(%q) = %v, want a refusal", tc.refused, c)
			continue
		}
		wantFieldError(t, err, "cell")
		if c != (ID{}) {
			t.Errorf("Parse(%q) returned %v beside its error", tc.refused, c)
		}
	}
}

// TestBoundaries checks every tenth-degree edge of the globe: the
// position exactly on an edge belongs to the cell whose south (west)
// edge it is, and the float64 just below belongs to the cell before.
// Georgia's box (41.0..43.6 N, 40.0..46.7 E), -180, 0, 180, -90 and 90
// are among them.
func TestBoundaries(t *testing.T) {
	for k := -900; k <= 900; k++ {
		edge := float64(k) / 10
		for _, lon := range []float64{-180, 0, 44.8} {
			c := mustOf(t, ll(edge, lon), Level5)
			wantIdx := k + 900
			if k == 900 {
				wantIdx = 1799 // +90 is in the last row
			}
			if c.LatIdx != wantIdx {
				t.Fatalf("Of(%v, %v).LatIdx = %d, want %d", edge, lon, c.LatIdx, wantIdx)
			}
			if k < 900 && c.BBox().MinLat != edge {
				t.Fatalf("Of(%v).BBox().MinLat = %v, want %v", edge, c.BBox().MinLat, edge)
			}
			if k > -900 {
				below := mustOf(t, ll(math.Nextafter(edge, -91), lon), Level5)
				if below.LatIdx != k+899 {
					t.Fatalf("Of(just below %v).LatIdx = %d, want %d", edge, below.LatIdx, k+899)
				}
			}
			// The same edge computed another way (k * 0.1) is within an
			// ulp of the decimal and lands in the same or the row below.
			alt := mustOf(t, ll(float64(k)*0.1, lon), Level5)
			if d := c.LatIdx - alt.LatIdx; d < 0 || d > 1 {
				t.Fatalf("Of(%v) and Of(%v) are %d rows apart", edge, float64(k)*0.1, d)
			}
		}
	}
	for k := -1800; k <= 1800; k++ {
		edge := float64(k) / 10
		c := mustOf(t, ll(41.7, edge), Level5)
		wantIdx := k + 1800
		if k == 1800 {
			wantIdx = 0 // 180 is -180
		}
		if c.LonIdx != wantIdx {
			t.Fatalf("Of(41.7, %v).LonIdx = %d, want %d", edge, c.LonIdx, wantIdx)
		}
		if k < 1800 && c.BBox().MinLon != edge {
			t.Fatalf("Of(41.7, %v).BBox().MinLon = %v", edge, c.BBox().MinLon)
		}
		if k > -1800 {
			below := mustOf(t, ll(41.7, math.Nextafter(edge, -181)), Level5)
			if below.LonIdx != k+1799 {
				t.Fatalf("Of(41.7, just below %v).LonIdx = %d, want %d", edge, below.LonIdx, k+1799)
			}
		}
	}
	// Whole degrees at Level3, with the same rule.
	for k := -90; k < 90; k++ {
		c := mustOf(t, ll(float64(k), 0), Level3)
		if c.LatIdx != k+90 || c.BBox().MinLat != float64(k) {
			t.Fatalf("Of(%d, 0) at c3 = %v", k, c)
		}
		if k > -90 {
			if b := mustOf(t, ll(math.Nextafter(float64(k), -91), 0), Level3); b.LatIdx != k+89 {
				t.Fatalf("Of(just below %d) at c3 = %v", k, b)
			}
		}
	}
	// 41.7 itself, the example of the brief: (41.7 + 90) * 10 is not
	// exactly representable.
	if c := mustOf(t, ll(41.7, 44.8), Level5); c.String() != "c5:1317:2248" || c.BBox().MinLat != 41.7 || c.BBox().MinLon != 44.8 {
		t.Fatalf("Of(41.7, 44.8) = %v %v", c, c.BBox())
	}
}

func TestRandomPositionsInsideTheirCell(t *testing.T) {
	rng := rand.New(rand.NewPCG(15, 1))
	for range 10_000 {
		p := ll(rng.Float64()*180-90, rng.Float64()*360-180)
		for _, l := range []Level{Level5, Level3} {
			c := mustOf(t, p, l)
			if !c.Valid() || !inCell(c, p) {
				t.Fatalf("%v is not inside Of at level %d: %v %v", p, l, c, c.BBox())
			}
		}
		if c5, c3 := mustOf(t, p, Level5), mustOf(t, p, Level3); c5.Parent() != c3 {
			t.Fatalf("%v: Parent(%v) = %v, want %v", p, c5, c5.Parent(), c3)
		}
	}
}

func TestLongitudeWrap(t *testing.T) {
	cases := []struct {
		lonDeg float64
		want   string
	}{
		{180, "c5:900:0"},
		{-180, "c5:900:0"},
		{540, "c5:900:0"},
		{360, "c5:900:1800"},
		{-190.05, "c5:900:3499"}, // 169.95
		{190.05, "c5:900:100"},   // -169.95
	}
	for _, tc := range cases {
		if c := mustOf(t, ll(0, tc.lonDeg), Level5); c.String() != tc.want {
			t.Errorf("Of(0, %v) = %s, want %s", tc.lonDeg, c, tc.want)
		}
	}
}

func TestOfRefusals(t *testing.T) {
	cases := []struct {
		p     core.LatLon
		l     Level
		field string
	}{
		{ll(math.NaN(), 0), Level5, "lat_deg"},
		{ll(math.Inf(1), 0), Level5, "lat_deg"},
		{ll(math.Inf(-1), 0), Level3, "lat_deg"},
		{ll(90.000001, 0), Level5, "lat_deg"},
		{ll(-90.000001, 0), Level3, "lat_deg"},
		{ll(0, math.NaN()), Level5, "lon_deg"},
		{ll(0, math.Inf(1)), Level5, "lon_deg"},
		{ll(0, math.Inf(-1)), Level3, "lon_deg"},
		{ll(0, 0), 0, "level"},
		{ll(0, 0), 4, "level"},
		{ll(math.NaN(), math.NaN()), 7, "level"},
	}
	for _, tc := range cases {
		c, err := Of(tc.p, tc.l)
		if err == nil {
			t.Errorf("Of(%v, %d) = %v, want a refusal", tc.p, tc.l, c)
			continue
		}
		wantFieldError(t, err, tc.field)
		if c != (ID{}) {
			t.Errorf("Of(%v, %d) returned %v beside its error", tc.p, tc.l, c)
		}
	}
	// The accepted twins: the extreme valid latitudes.
	for _, lat := range []float64{90, -90} {
		if _, err := Of(ll(lat, 0), Level5); err != nil {
			t.Errorf("Of(%v, 0): %v", lat, err)
		}
	}
}

// isNeighbour reports whether two cells of a level share an edge or a
// corner, judged on their boxes with the antimeridian wrapped.
func isNeighbour(a, b ID) bool {
	ba, bb := a.BBox(), b.BBox()
	latTouch := ba.MaxLat >= bb.MinLat && bb.MaxLat >= ba.MinLat
	lonTouch := ba.MaxLon >= bb.MinLon && bb.MaxLon >= ba.MinLon ||
		(ba.MaxLon == 180 && bb.MinLon == -180) || (bb.MaxLon == 180 && ba.MinLon == -180)
	return latTouch && lonTouch && a != b
}

func TestRing1(t *testing.T) {
	cases := []struct {
		c    ID
		want []string
	}{
		{ID{Level5, 1317, 2248}, []string{
			"c5:1316:2247", "c5:1316:2248", "c5:1316:2249",
			"c5:1317:2247", "c5:1317:2249",
			"c5:1318:2247", "c5:1318:2248", "c5:1318:2249",
		}},
		{ID{Level5, 900, 0}, []string{
			"c5:899:0", "c5:899:1", "c5:899:3599",
			"c5:900:1", "c5:900:3599",
			"c5:901:0", "c5:901:1", "c5:901:3599",
		}},
		{ID{Level5, 900, 3599}, []string{
			"c5:899:0", "c5:899:3598", "c5:899:3599",
			"c5:900:0", "c5:900:3598",
			"c5:901:0", "c5:901:3598", "c5:901:3599",
		}},
		{ID{Level5, 0, 0}, []string{
			"c5:0:1", "c5:0:3599",
			"c5:1:0", "c5:1:1", "c5:1:3599",
		}},
		{ID{Level3, 179, 100}, []string{
			"c3:178:99", "c3:178:100", "c3:178:101",
			"c3:179:99", "c3:179:101",
		}},
	}
	for _, tc := range cases {
		got := tc.c.Ring1()
		if len(got) != len(tc.want) {
			t.Fatalf("Ring1(%v) = %v, want %v", tc.c, got, tc.want)
		}
		for i := range got {
			if got[i].String() != tc.want[i] {
				t.Fatalf("Ring1(%v)[%d] = %v, want %v", tc.c, i, got[i], tc.want[i])
			}
		}
	}
	// Every c3 cell and the c5 cells of the first, middle and last rows:
	// count, order, never itself, each neighbour touches.
	checkRing := func(c ID) {
		r := c.Ring1()
		want := 8
		if c.LatIdx == 0 || c.LatIdx == c.Level.rows()-1 {
			want = 5
		}
		if len(r) != want {
			t.Fatalf("Ring1(%v) has %d cells, want %d", c, len(r), want)
		}
		for i, n := range r {
			if n == c || !n.Valid() || n.Level != c.Level {
				t.Fatalf("Ring1(%v) holds %v", c, n)
			}
			if !isNeighbour(c, n) {
				t.Fatalf("Ring1(%v): %v does not touch it (%v, %v)", c, n, c.BBox(), n.BBox())
			}
			if i > 0 && (r[i-1].LatIdx > n.LatIdx || r[i-1].LatIdx == n.LatIdx && r[i-1].LonIdx >= n.LonIdx) {
				t.Fatalf("Ring1(%v) is not sorted: %v", c, r)
			}
		}
	}
	for i := range 180 {
		for j := range 360 {
			checkRing(ID{Level3, i, j})
		}
	}
	for _, i := range []int{0, 1, 900, 1798, 1799} {
		for j := range 3600 {
			checkRing(ID{Level5, i, j})
		}
	}
}

func TestParentChildren(t *testing.T) {
	for i := range 180 {
		for j := range 360 {
			p := ID{Level3, i, j}
			if p.Parent() != p {
				t.Fatalf("Parent(%v) = %v, want itself", p, p.Parent())
			}
			kids := p.Children()
			if len(kids) != 100 {
				t.Fatalf("Children(%v) has %d cells", p, len(kids))
			}
			pb := p.BBox()
			for k, c := range kids {
				if c.Level != Level5 || c.Parent() != p {
					t.Fatalf("child %v of %v has parent %v", c, p, c.Parent())
				}
				if c.Children() != nil {
					t.Fatalf("Children(%v) of a c5 = %v, want nil", c, c.Children())
				}
				// Row-major order and an exact tiling of the parent's box.
				row, col := k/10, k%10
				cb := c.BBox()
				if c.LatIdx != 10*i+row || c.LonIdx != 10*j+col {
					t.Fatalf("Children(%v)[%d] = %v", p, k, c)
				}
				if row == 0 && cb.MinLat != pb.MinLat || row == 9 && cb.MaxLat != pb.MaxLat ||
					col == 0 && cb.MinLon != pb.MinLon || col == 9 && cb.MaxLon != pb.MaxLon {
					t.Fatalf("child %v box %v does not meet parent %v box %v", c, cb, p, pb)
				}
				if col < 9 && kids[k+1].BBox().MinLon != cb.MaxLon || row < 9 && kids[k+10].BBox().MinLat != cb.MaxLat {
					t.Fatalf("children of %v leave a gap or overlap at %v", p, c)
				}
			}
		}
	}
}

func TestBBoxAndCentre(t *testing.T) {
	c := ID{Level5, 1317, 2248}
	b := c.BBox()
	if b != (geodesy.BBox{MinLat: 41.7, MinLon: 44.8, MaxLat: 41.8, MaxLon: 44.9}) {
		t.Fatalf("BBox(%v) = %+v", c, b)
	}
	if last := (ID{Level5, 1799, 3599}).BBox(); last.MaxLon != 180 || last.MaxLat != 90 {
		t.Fatalf("last cell box = %+v, want east 180 and north 90", last)
	}
	if ctr := c.Centre(); ctr != ll(41.75, 44.85) {
		t.Fatalf("Centre(%v) = %v", c, ctr)
	}
	if ctr := (ID{Level3, 0, 0}).Centre(); ctr != ll(-89.5, -179.5) {
		t.Fatalf("Centre(c3:0:0) = %v", ctr)
	}
	for _, l := range []Level{Level3, Level5} {
		for i := range l.rows() {
			for _, j := range []int{0, 1, l.cols() / 2, l.cols() - 1} {
				c := ID{l, i, j}
				ctr := c.Centre()
				if !ctr.Valid() || mustOf(t, ctr, l) != c {
					t.Fatalf("Of(Centre(%v) = %v) = %v", c, ctr, mustOf(t, ctr, l))
				}
			}
		}
	}
}

// TestInvalidID runs every method on invalid IDs beside a valid one.
func TestInvalidID(t *testing.T) {
	valid := ID{Level5, 1317, 2248}
	if !valid.Valid() || valid.String() == "" || valid.Parent() == (ID{}) || valid.Ring1() == nil || !valid.Centre().Valid() {
		t.Fatalf("valid %v must answer every method", valid)
	}
	if (ID{Level3, 131, 224}).Children() == nil {
		t.Fatal("a valid c3 has children")
	}
	for _, c := range []ID{{}, {Level5, -1, 0}, {Level5, 1800, 0}, {Level5, 0, -1}, {Level5, 0, 3600}, {Level3, 180, 0}, {Level3, 0, 360}, {4, 0, 0}} {
		if c.Valid() || c.String() != "" || c.Parent() != (ID{}) || c.Children() != nil || c.Ring1() != nil || c.Centre().Valid() {
			t.Errorf("invalid %v answered a method", c)
		}
		if b := c.BBox(); b.MinLat <= b.MaxLat || b.Contains(ll(0, 0)) {
			t.Errorf("BBox(%v) = %+v, want empty", c, b)
		}
	}
}

func TestCover(t *testing.T) {
	// A box inside one cell returns that cell.
	got, err := Cover(geodesy.BBox{MinLat: 41.71, MinLon: 44.81, MaxLat: 41.72, MaxLon: 44.82}, Level5, MaxCoverDefault)
	if err != nil || len(got) != 1 || got[0].String() != "c5:1317:2248" {
		t.Fatalf("Cover(one cell) = %v, %v", got, err)
	}
	// Georgia: rows 1310..1336 (27) by columns 2200..2267 (68) at c5,
	// the edges at 43.6 and 46.7 included; rows 131..133 (3) by columns
	// 220..226 (7) at c3.
	georgia := geodesy.BBox{MinLat: 41.0, MinLon: 40.0, MaxLat: 43.6, MaxLon: 46.7}
	got, err = Cover(georgia, Level5, MaxCoverDefault)
	if err != nil || len(got) != 27*68 {
		t.Fatalf("Cover(Georgia, c5) = %d cells, %v; want %d", len(got), err, 27*68)
	}
	if got[0].String() != "c5:1310:2200" || got[len(got)-1].String() != "c5:1336:2267" {
		t.Fatalf("Cover(Georgia, c5) spans %v..%v", got[0], got[len(got)-1])
	}
	assertSorted(t, got)
	got, err = Cover(georgia, Level3, MaxCoverDefault)
	if err != nil || len(got) != 3*7 || got[0].String() != "c3:131:220" || got[20].String() != "c3:133:226" {
		t.Fatalf("Cover(Georgia, c3) = %v, %v; want 21 cells c3:131:220..c3:133:226", got, err)
	}
	// Every random position of the box is in a covered cell.
	set := map[ID]bool{}
	c5s, _ := Cover(georgia, Level5, MaxCoverDefault)
	for _, c := range c5s {
		set[c] = true
	}
	rng := rand.New(rand.NewPCG(4, 1))
	for range 2000 {
		p := ll(41+rng.Float64()*2.6, 40+rng.Float64()*6.7)
		if !set[mustOf(t, p, Level5)] {
			t.Fatalf("%v in the Georgia box is not covered", p)
		}
	}
}

func assertSorted(t *testing.T, ids []ID) {
	t.Helper()
	for i := 1; i < len(ids); i++ {
		a, b := ids[i-1], ids[i]
		if a.LatIdx > b.LatIdx || a.LatIdx == b.LatIdx && a.LonIdx >= b.LonIdx {
			t.Fatalf("not sorted at %d: %v then %v", i, a, b)
		}
	}
}

func TestCoverAntimeridian(t *testing.T) {
	got, err := Cover(geodesy.BBox{MinLat: 0.05, MinLon: 179.85, MaxLat: 0.15, MaxLon: -179.85}, Level5, MaxCoverDefault)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"c5:900:0", "c5:900:1", "c5:900:3598", "c5:900:3599",
		"c5:901:0", "c5:901:1", "c5:901:3598", "c5:901:3599",
	}
	if len(got) != len(want) {
		t.Fatalf("Cover(antimeridian) = %v, want %v", got, want)
	}
	for i := range got {
		if got[i].String() != want[i] {
			t.Fatalf("Cover(antimeridian)[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	assertSorted(t, got)
	// The twin that does not cross: the same longitudes as a plain box
	// span the globe the other way, columns 1..3598 and not the edges.
	got, err = Cover(geodesy.BBox{MinLat: 0.05, MinLon: -179.85, MaxLat: 0.05, MaxLon: 179.85}, Level5, MaxCoverDefault)
	if err != nil || len(got) != 3598 || got[0].LonIdx != 1 || got[len(got)-1].LonIdx != 3598 {
		t.Fatalf("Cover(-179.85..179.85) = %d cells, %v; want columns 1..3598", len(got), err)
	}
	// A crossing box whose two ranges meet covers each column once.
	got, err = Cover(geodesy.BBox{MinLat: 0.05, MinLon: 10.05, MaxLat: 0.05, MaxLon: 10.01}, Level5, MaxCoverDefault)
	if err != nil || len(got) != 3600 {
		t.Fatalf("Cover(10.05..10.01 crossing) = %d cells, %v; want 3600", len(got), err)
	}
	assertSorted(t, got)
	// A crossing box whose ranges are adjacent, not overlapping.
	got, err = Cover(geodesy.BBox{MinLat: 0.05, MinLon: 10.15, MaxLat: 0.05, MaxLon: 10.05}, Level5, MaxCoverDefault)
	if err != nil || len(got) != 3600 {
		t.Fatalf("Cover(10.15..10.05 crossing) = %d cells, %v; want 3600", len(got), err)
	}
	// A crossing box with a gap leaves the gap out.
	got, err = Cover(geodesy.BBox{MinLat: 0.05, MinLon: 10.25, MaxLat: 0.05, MaxLon: 10.05}, Level5, MaxCoverDefault)
	if err != nil || len(got) != 3599 {
		t.Fatalf("Cover(10.25..10.05 crossing) = %d cells, %v; want 3599", len(got), err)
	}
	for _, c := range got {
		if c.LonIdx == 1901 {
			t.Fatalf("the gap column 1901 is covered")
		}
	}
}

func TestCoverEdges(t *testing.T) {
	// MaxLon == 180 includes the last column once and column 0, where Of
	// places a position on the 180 meridian.
	got, err := Cover(geodesy.BBox{MinLat: 0.05, MinLon: 179.95, MaxLat: 0.05, MaxLon: 180}, Level5, MaxCoverDefault)
	if err != nil || len(got) != 2 || got[0].String() != "c5:900:0" || got[1].String() != "c5:900:3599" {
		t.Fatalf("Cover(179.95..180) = %v, %v; want c5:900:0 and c5:900:3599", got, err)
	}
	// Its twin: a box stopping short of 180 does not take column 0.
	got, err = Cover(geodesy.BBox{MinLat: 0.05, MinLon: 179.95, MaxLat: 0.05, MaxLon: 179.99}, Level5, MaxCoverDefault)
	if err != nil || len(got) != 1 || got[0].String() != "c5:900:3599" {
		t.Fatalf("Cover(179.95..179.99) = %v, %v; want only c5:900:3599", got, err)
	}
	// The coordinator's case: Of(41.7, 180) is c5:1317:0 and is covered.
	if c := mustOf(t, ll(41.7, 180), Level5); c.String() != "c5:1317:0" {
		t.Fatalf("Of(41.7, 180) = %s", c)
	}
	got, err = Cover(geodesy.BBox{MinLat: 41.7, MinLon: 179.9, MaxLat: 41.7, MaxLon: 180}, Level5, 2)
	if err != nil || !containsID(got, ID{Level5, 1317, 0}) {
		t.Fatalf("Cover(41.7, 179.9..180) = %v, %v; want c5:1317:0 in it", got, err)
	}
	// The whole globe at c3: every cell once, 90 in the last row.
	got, err = Cover(geodesy.BBox{MinLat: -90, MinLon: -180, MaxLat: 90, MaxLon: 180}, Level3, 180*360)
	if err != nil || len(got) != 180*360 {
		t.Fatalf("Cover(globe, c3) = %d cells, %v; want %d", len(got), err, 180*360)
	}
	assertSorted(t, got)
	// A degenerate box (one point) is one cell.
	got, err = Cover(geodesy.BBox{MinLat: 41.7, MinLon: 44.8, MaxLat: 41.7, MaxLon: 44.8}, Level5, 1)
	if err != nil || len(got) != 1 || got[0].String() != "c5:1317:2248" {
		t.Fatalf("Cover(point) = %v, %v", got, err)
	}
}

// TestCoverBound exceeds the bound (E-10) beside the same box within it.
func TestCoverBound(t *testing.T) {
	two := geodesy.BBox{MinLat: 41.75, MinLon: 44.75, MaxLat: 41.75, MaxLon: 44.85}
	got, err := Cover(two, Level5, 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("Cover(two cells, max 2) = %v, %v", got, err)
	}
	got, err = Cover(two, Level5, 1)
	if got != nil {
		t.Fatalf("Cover(two cells, max 1) = %v, want nil", got)
	}
	wantFieldError(t, err, "bbox")
	// The globe at c5 is 6.48 million cells: refused without allocating.
	globe := geodesy.BBox{MinLat: -90, MinLon: -180, MaxLat: 90, MaxLon: 180}
	allocs := testing.AllocsPerRun(10, func() {
		if got, err := Cover(globe, Level5, 1); got != nil || err == nil {
			t.Fatal("Cover(globe, max 1) must be refused")
		}
	})
	// The error itself allocates (FieldError and its reason); the
	// 6.48 million IDs would be one allocation of 155 MB, which the
	// allocation count alone cannot tell apart, so check the bytes too.
	var ms0, ms1 runtime.MemStats
	runtime.ReadMemStats(&ms0)
	_, _ = Cover(globe, Level5, 1)
	runtime.ReadMemStats(&ms1)
	if allocs > 10 || ms1.TotalAlloc-ms0.TotalAlloc > 1<<16 {
		t.Fatalf("a refused Cover allocated %v times, %d bytes", allocs, ms1.TotalAlloc-ms0.TotalAlloc)
	}
	got, err = Cover(globe, Level5, MaxCoverDefault)
	if got != nil {
		t.Fatalf("Cover(globe, c5, MaxCoverDefault) returned %d cells", len(got))
	}
	wantFieldError(t, err, "bbox")
}

func TestCoverRefusals(t *testing.T) {
	ok := geodesy.BBox{MinLat: 41, MinLon: 44, MaxLat: 41.1, MaxLon: 44.1}
	if _, err := Cover(ok, Level5, MaxCoverDefault); err != nil {
		t.Fatalf("Cover(ok) = %v", err)
	}
	nan, inf := math.NaN(), math.Inf(1)
	cases := []struct {
		b     geodesy.BBox
		l     Level
		max   int
		field string
	}{
		{ok, 0, MaxCoverDefault, "level"},
		{ok, Level5, 0, "max"},
		{ok, Level5, -1, "max"},
		{geodesy.BBox{MinLat: nan, MinLon: 44, MaxLat: 41.1, MaxLon: 44.1}, Level5, MaxCoverDefault, "bbox"},
		{geodesy.BBox{MinLat: 41, MinLon: inf, MaxLat: 41.1, MaxLon: 44.1}, Level5, MaxCoverDefault, "bbox"},
		{geodesy.BBox{MinLat: 41, MinLon: 44, MaxLat: -inf, MaxLon: 44.1}, Level5, MaxCoverDefault, "bbox"},
		{geodesy.BBox{MinLat: 41, MinLon: 44, MaxLat: 41.1, MaxLon: nan}, Level5, MaxCoverDefault, "bbox"},
		{geodesy.BBox{MinLat: -90.5, MinLon: 44, MaxLat: 41.1, MaxLon: 44.1}, Level5, MaxCoverDefault, "bbox"},
		{geodesy.BBox{MinLat: 41, MinLon: 44, MaxLat: 90.5, MaxLon: 44.1}, Level5, MaxCoverDefault, "bbox"},
		{geodesy.BBox{MinLat: 41.2, MinLon: 44, MaxLat: 41.1, MaxLon: 44.1}, Level5, MaxCoverDefault, "bbox"},
		{geodesy.BBox{MinLat: 41, MinLon: -180.5, MaxLat: 41.1, MaxLon: 44.1}, Level5, MaxCoverDefault, "bbox"},
		{geodesy.BBox{MinLat: 41, MinLon: 180.5, MaxLat: 41.1, MaxLon: 44.1}, Level5, MaxCoverDefault, "bbox"},
		{geodesy.BBox{MinLat: 41, MinLon: 44, MaxLat: 41.1, MaxLon: -180.5}, Level5, MaxCoverDefault, "bbox"},
		{geodesy.BBox{MinLat: 41, MinLon: 44, MaxLat: 41.1, MaxLon: 180.5}, Level5, MaxCoverDefault, "bbox"},
		{emptyBBox, Level5, MaxCoverDefault, "bbox"},
	}
	for _, tc := range cases {
		got, err := Cover(tc.b, tc.l, tc.max)
		if err == nil || got != nil {
			t.Errorf("Cover(%+v, %d, %d) = %v, %v; want a refusal", tc.b, tc.l, tc.max, got, err)
			continue
		}
		wantFieldError(t, err, tc.field)
	}
}

// TestFloorCorrection pins a position where the plain floor is wrong and
// the correction against the cell's own edges is what places it: one
// ulp west of -127.8, the product with 10 rounds to exactly -1278, so
// floor alone would put it in the column whose west edge is -127.8.
func TestFloorCorrection(t *testing.T) {
	lon := math.Nextafter(-127.8, math.Inf(-1))
	if math.Floor(lon*10) != -1278 {
		t.Fatalf("floor(%v * 10) = %v; the case no longer exercises the correction", lon, math.Floor(lon*10))
	}
	c := mustOf(t, ll(41.7, lon), Level5)
	if c.LonIdx != 521 || c.BBox().MaxLon != -127.8 || !inCell(c, ll(41.7, lon)) {
		t.Fatalf("Of(41.7, %v) = %v %+v, want column 521 ending at -127.8", lon, c, c.BBox())
	}
	// The twin on the edge itself needs no correction.
	if c := mustOf(t, ll(41.7, -127.8), Level5); c.LonIdx != 522 {
		t.Fatalf("Of(41.7, -127.8) = %v, want column 522", c)
	}
	// The clamp and the name of an invalid level, unreachable through
	// the exported API, answer as documented.
	if clamp(-1, 9) != 0 || clamp(10, 9) != 9 || clamp(4, 9) != 4 || Level(4).prefix() != "" {
		t.Fatal("clamp or prefix misbehaves")
	}
}

func containsID(ids []ID, c ID) bool {
	for _, x := range ids {
		if x == c {
			return true
		}
	}
	return false
}

// The invariant: every position p inside a box has Of(p) in Cover(box),
// for boxes on the meridians -180, 0 and 180, across the antimeridian,
// and at random; the points include every corner and both meridian
// spellings of 180.
func TestCoverHoldsEveryCellOfItsPositions(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	boxes := []geodesy.BBox{
		{MinLat: 41.7, MinLon: 179.9, MaxLat: 41.8, MaxLon: 180},
		{MinLat: 41.7, MinLon: -180, MaxLat: 41.8, MaxLon: -179.9},
		{MinLat: -90, MinLon: 179, MaxLat: 90, MaxLon: 180},
		{MinLat: 0, MinLon: 180, MaxLat: 1, MaxLon: 180},
		{MinLat: 0, MinLon: 179.95, MaxLat: 0.3, MaxLon: -179.95},
		{MinLat: 0, MinLon: 180, MaxLat: 0.3, MaxLon: -180},
		{MinLat: 89.9, MinLon: -0.1, MaxLat: 90, MaxLon: 0.1},
		{MinLat: 41, MinLon: 40, MaxLat: 43.6, MaxLon: 46.7},
	}
	for range 300 {
		// Boxes up to 10 degrees a side, so that a c5 cover stays small;
		// one in four reaches 180, and those that pass it cross.
		s := rng.Float64()*170 - 90
		n := math.Min(90, s+rng.Float64()*10)
		w := rng.Float64()*360 - 180
		e := w + rng.Float64()*10
		switch {
		case rng.IntN(4) == 0:
			w, e = 180-rng.Float64()*10, 180
		case e > 180:
			e -= 360
		}
		boxes = append(boxes, geodesy.BBox{MinLat: s, MinLon: w, MaxLat: n, MaxLon: e})
	}
	for _, b := range boxes {
		lons := []float64{b.MinLon, b.MaxLon}
		lats := []float64{b.MinLat, b.MaxLat, (b.MinLat + b.MaxLat) / 2}
		for range 20 {
			if b.MinLon <= b.MaxLon {
				lons = append(lons, b.MinLon+rng.Float64()*(b.MaxLon-b.MinLon))
			} else {
				lons = append(lons, b.MinLon+rng.Float64()*(180-b.MinLon), -180+rng.Float64()*(b.MaxLon+180))
			}
		}
		if b.MaxLon == 180 || b.MinLon > b.MaxLon {
			lons = append(lons, 180, -180)
		}
		for _, l := range []Level{Level5, Level3} {
			cover, err := Cover(b, l, 1_000_000)
			if err != nil {
				t.Fatalf("Cover(%+v, %d): %v", b, l, err)
			}
			got := make(map[ID]bool, len(cover))
			for _, c := range cover {
				got[c] = true
			}
			for _, lat := range lats {
				for _, lon := range lons {
					c := mustOf(t, ll(lat, lon), l)
					if !got[c] {
						t.Fatalf("Of(%v, %v) = %s is not in Cover(%+v)", lat, lon, c, b)
					}
				}
			}
		}
	}
}
