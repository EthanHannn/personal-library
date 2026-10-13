package reader

import (
	"sort"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Compatibility deltas against x/text v0.43.0, derived from the previous
// ec14e64 image's TextDecoder (Node 20.20.2, ICU 78.2). These are character
// mapping facts, not copied application code. tools/m0/encoding-reference.mjs
// records reference hashes for all two-byte and BMP four-byte sequences.
// Besides private-use ranges, current GB18030 maps vertical punctuation and
// several ideographs differently from x/text's older GBK table.
var gbPairOverrides = []struct {
	start, end int
	first      rune
}{
	{6080, 6175, 58566}, {6270, 6365, 58662}, {6376, 6381, 59238},
	{6433, 6433, 59245}, {6444, 6445, 59246}, {6458, 6459, 59248},
	{6460, 6554, 58758}, {6650, 6745, 58854}, {6829, 6839, 59250},
	{6840, 6935, 58950}, {7022, 7029, 59261}, {7030, 7125, 59046},
	{7150, 7157, 59269}, {7182, 7182, 65040}, {7183, 7183, 65042},
	{7184, 7184, 65041}, {7185, 7188, 65043}, {7201, 7202, 65047},
	{7208, 7208, 65049}, {7211, 7219, 59287}, {7220, 7315, 59142},
	{7349, 7363, 59296}, {7397, 7409, 59311}, {7495, 7505, 59324},
	{7533, 7533, 7743}, {7538, 7541, 59337}, {7579, 7599, 59341},
	{7624, 7624, 59362}, {7627, 7627, 59363}, {7629, 7631, 59364},
	{7686, 7698, 59380}, {7775, 7789, 59393}, {7886, 7979, 57344},
	{8076, 8169, 57438}, {8266, 8359, 57532}, {8456, 8549, 57626},
	{8646, 8739, 57720}, {8836, 8929, 57814}, {16525, 16529, 59408},
	{22706, 22799, 57908}, {22896, 22989, 58002}, {23086, 23179, 58096},
	{23276, 23369, 58190}, {23466, 23559, 58284}, {23656, 23749, 58378},
	{23767, 23769, 59414}, {23775, 23775, 40884}, {23783, 23783, 40885},
	{23788, 23789, 40886}, {23794, 23794, 59441}, {23795, 23795, 40888},
	{23804, 23804, 59451}, {23812, 23812, 40889}, {23829, 23829, 40890},
	{23830, 23830, 59477}, {23845, 23845, 40891}, {23846, 23939, 58472},
}

func gbOverride(src []byte) (rune, bool) {
	if len(src) == 2 {
		trail := int(src[1]) - 0x40
		if src[1] > 0x7f {
			trail--
		}
		pointer := (int(src[0])-0x81)*190 + trail
		i := sort.Search(len(gbPairOverrides), func(i int) bool { return gbPairOverrides[i].end >= pointer })
		if i < len(gbPairOverrides) && gbPairOverrides[i].start <= pointer {
			return gbPairOverrides[i].first + rune(pointer-gbPairOverrides[i].start), true
		}
	} else if len(src) == 4 {
		pointer := (((int(src[0])-0x81)*10+int(src[1])-0x30)*126+int(src[2])-0x81)*10 + int(src[3]) - 0x30
		if pointer == 7457 {
			return '\ue7c7', true
		}
	}
	return 0, false
}

func decodeGB(src []byte, limit int) ([]byte, error) {
	dst := make([]byte, 0, min(limit, len(src)*3))
	decoder := simplifiedchinese.GB18030.NewDecoder()
	appendSegment := func(segment []byte) error {
		if len(segment) == 0 {
			return nil
		}
		n, used, err := decoder.Transform(dst[len(dst):cap(dst)], segment, true)
		if err != nil || used != len(segment) {
			return ErrEncoding
		}
		dst = dst[:len(dst)+n]
		return nil
	}
	segment := 0
	for i := 0; i < len(src); {
		width, _, err := gbCharacter(src[i:])
		if err != nil {
			return nil, ErrEncoding
		}
		if ch, ok := gbOverride(src[i : i+width]); ok {
			if err := appendSegment(src[segment:i]); err != nil {
				return nil, err
			}
			dst = utf8.AppendRune(dst, ch)
			segment = i + width
		}
		i += width
	}
	if err := appendSegment(src[segment:]); err != nil {
		return nil, err
	}
	return dst, nil
}
