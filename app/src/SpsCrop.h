#ifndef SPSCROP_H
#define SPSCROP_H

#include "Nv12Rotate.h"

#include <stddef.h>
#include <vector>

// Cropping the picture an H.264 stream declares, by rewriting its SPS (frame_cropping): our camera
// keeps encoding its own size, upright, and the peer's decoder shows only the centred part, e.g.
// 1280x720 as 404x720 (9:16). Header-only and free of Qt so app/tests/sps_crop_test.cpp builds on the Mac.

// The fields of an SPS that the crop needs. Bit positions count from the first bit of `rbsp`.
struct SpsInfo
{
    std::vector<unsigned char> rbsp; // the bytes after the NAL header, emulation prevention removed
    int width, height;               // the coded picture in luma samples (before any crop)
    int cropUnitY;                   // luma rows per frame_crop_top/bottom_offset: 2, or 4 for field coding
    unsigned crop[4];                // frame_crop left, right, top, bottom offsets (0 without frame_cropping_flag)
    size_t cropFlagPos;              // just before frame_cropping_flag
    size_t restPos;                  // just after the crop fields: vui_parameters_present_flag follows
};

namespace spscrop {

// u(n), ue(v) and se(v) over a byte vector, most significant bit first. Reading past the end
// clears ok() and returns 0.
class Bits
{
public:
    explicit Bits(const std::vector<unsigned char> &d) : m_d(d), m_pos(0), m_ok(true) {}
    unsigned bit()
    {
        if (m_pos >= m_d.size() * 8) {
            m_ok = false;
            return 0;
        }
        const unsigned b = (m_d[m_pos >> 3] >> (7 - (m_pos & 7))) & 1;
        m_pos++;
        return b;
    }
    unsigned u(int n)
    {
        unsigned v = 0;
        while (n-- > 0)
            v = (v << 1) | bit();
        return v;
    }
    unsigned ue()
    {
        int zeros = 0;
        while (m_ok && bit() == 0) {
            if (++zeros > 31)
                m_ok = false;
        }
        return m_ok ? ((1u << zeros) - 1) + u(zeros) : 0;
    }
    int se()
    {
        const unsigned k = ue();
        return (k & 1) ? int((k + 1) / 2) : -int(k / 2);
    }
    size_t pos() const { return m_pos; }
    bool ok() const { return m_ok; }
private:
    const std::vector<unsigned char> &m_d;
    size_t m_pos;
    bool m_ok;
};

inline unsigned bitAt(const std::vector<unsigned char> &d, size_t pos)
{
    return (d[pos >> 3] >> (7 - (pos & 7))) & 1;
}

// A growing bit string; the last byte is padded with 0 bits.
class Writer
{
public:
    Writer() : m_n(0) {}
    void bit(unsigned b)
    {
        if ((m_n & 7) == 0)
            m_buf.push_back(0);
        if (b)
            m_buf.back() |= 0x80 >> (m_n & 7);
        m_n++;
    }
    void ue(unsigned v)
    {
        const unsigned long long code = (unsigned long long)v + 1;
        int len = 0;
        while ((code >> len) > 1)
            len++;
        for (int i = 0; i < len; i++)
            bit(0);
        for (int i = len; i >= 0; i--)
            bit(unsigned(code >> i) & 1);
    }
    void copy(const std::vector<unsigned char> &d, size_t from, size_t to)
    {
        for (size_t p = from; p < to; p++)
            bit(bitAt(d, p));
    }
    const std::vector<unsigned char> &bytes() const { return m_buf; }
private:
    std::vector<unsigned char> m_buf;
    size_t m_n;
};

} // namespace spscrop

// Parses an SPS NAL (starting at its header byte, no start code, emulation prevention bytes still in)
// as far as frame_cropping. False for what the crop can't handle: another chroma format than 4:2:0,
// scaling matrices, a truncated or absurd SPS.
inline bool spsParse(const unsigned char *nal, int len, SpsInfo *s)
{
    if (len < 5 || (nal[0] & 31) != 7)
        return false;
    s->rbsp.clear();
    int zeros = 0;
    for (int i = 1; i < len; i++) {
        if (zeros >= 2 && nal[i] == 3) { // 00 00 03: the 03 is emulation prevention
            zeros = 0;
            continue;
        }
        s->rbsp.push_back(nal[i]);
        zeros = nal[i] == 0 ? zeros + 1 : 0;
    }

    spscrop::Bits r(s->rbsp);
    const unsigned profile = r.u(8);
    r.u(8); // constraint flags
    r.u(8); // level_idc
    r.ue(); // seq_parameter_set_id
    switch (profile) {
    case 100: case 110: case 122: case 244: case 44: case 83: case 86: case 118: case 128:
    case 138: case 139: case 134: case 135:
        if (r.ue() != 1) // chroma_format_idc: 4:2:0 only (the crop units below assume it)
            return false;
        r.ue(); // bit_depth_luma_minus8
        r.ue(); // bit_depth_chroma_minus8
        r.bit(); // qpprime_y_zero_transform_bypass_flag
        if (r.bit()) // seq_scaling_matrix_present_flag
            return false;
        break;
    default:
        break;
    }
    r.ue(); // log2_max_frame_num_minus4
    const unsigned pocType = r.ue();
    if (pocType == 0) {
        r.ue(); // log2_max_pic_order_cnt_lsb_minus4
    } else if (pocType == 1) {
        r.bit(); // delta_pic_order_always_zero_flag
        r.se();  // offset_for_non_ref_pic
        r.se();  // offset_for_top_to_bottom_field
        const unsigned n = r.ue();
        for (unsigned i = 0; i < n && r.ok(); i++)
            r.se(); // offset_for_ref_frame
    }
    r.ue();  // max_num_ref_frames
    r.bit(); // gaps_in_frame_num_value_allowed_flag
    const unsigned widthMbs = r.ue() + 1;
    const unsigned heightMapUnits = r.ue() + 1;
    const unsigned frameMbsOnly = r.bit();
    if (!frameMbsOnly)
        r.bit(); // mb_adaptive_frame_field_flag
    r.bit(); // direct_8x8_inference_flag
    s->cropFlagPos = r.pos();
    for (int i = 0; i < 4; i++)
        s->crop[i] = 0;
    if (r.bit()) {
        for (int i = 0; i < 4; i++)
            s->crop[i] = r.ue();
    }
    s->restPos = r.pos();
    if (!r.ok() || widthMbs > 1024 || heightMapUnits > 1024)
        return false;
    for (int i = 0; i < 4; i++) {
        if (s->crop[i] > 16384)
            return false;
    }
    s->width = int(widthMbs) * 16;
    s->height = int(heightMapUnits) * 16 * (frameMbsOnly ? 1 : 2);
    s->cropUnitY = frameMbsOnly ? 2 : 4;
    return true;
}

// nal: one SPS NAL unit starting at its header byte (0x67), no start code, emulation prevention bytes
// included. Rewrites frame_cropping so the visible picture is the centred part with aspect aw:ah
// (centreCrop from Nv12Rotate.h, applied to the size already visible, so an existing crop such as
// 1088->1080 is kept and added to). *out is the new NAL (same form), *outW x *outH the size it shows.
// False, everything untouched, when the SPS can't be handled or already has that shape.
// ponytail: "already that shape" is within 4 px, not exact: centreCrop's even sides would trim a
// picture it just cropped (404x720 to 404x718) and never settle.
inline bool spsCropToAspect(const unsigned char *nal, int len, int aw, int ah, std::vector<unsigned char> *out,
                            int *outW, int *outH)
{
    SpsInfo s;
    if (!spsParse(nal, len, &s))
        return false;
    const int unitY = s.cropUnitY;
    const int vw = s.width - 2 * int(s.crop[0] + s.crop[1]);
    const int vh = s.height - unitY * int(s.crop[2] + s.crop[3]);
    if (vw <= 0 || vh <= 0)
        return false;
    int x, y, cw, ch;
    centreCrop(vw, vh, aw, ah, &x, &y, &cw, &ch);
    if (cw <= 0 || ch <= 0 || (vw - cw <= 4 && vh - ch <= 4))
        return false;
    // centreCrop's values are even, so the halves are exact; vertically with field coding they floor.
    const unsigned c[4] = { s.crop[0] + unsigned(x / 2), s.crop[1] + unsigned((vw - x - cw) / 2),
                            s.crop[2] + unsigned(y / unitY), s.crop[3] + unsigned((vh - y - ch) / unitY) };

    // The rbsp_stop_one_bit: the last 1 bit, behind any zero bytes.
    size_t last = s.rbsp.size();
    while (last > 0 && s.rbsp[last - 1] == 0)
        last--;
    if (last == 0)
        return false;
    size_t stop = last * 8 - 1; // the position of the last byte's lowest bit, moved up past its zeros
    while (!spscrop::bitAt(s.rbsp, stop))
        stop--;
    if (stop < s.restPos)
        return false;

    spscrop::Writer w;
    w.copy(s.rbsp, 0, s.cropFlagPos);
    w.bit(1); // frame_cropping_flag
    for (int i = 0; i < 4; i++)
        w.ue(c[i]);
    w.copy(s.rbsp, s.restPos, stop);
    w.bit(1); // rbsp_stop_one_bit; the last byte is padded with zeros

    std::vector<unsigned char> result;
    result.push_back(nal[0]);
    int zeros = 0;
    for (size_t i = 0; i < w.bytes().size(); i++) {
        const unsigned char b = w.bytes()[i];
        if (zeros >= 2 && b <= 3) { // would read as a start code or an escape: 00 00 03 first
            result.push_back(3);
            zeros = 0;
        }
        result.push_back(b);
        zeros = b == 0 ? zeros + 1 : 0;
    }
    out->swap(result);
    *outW = s.width - 2 * int(c[0] + c[1]);
    *outH = s.height - unitY * int(c[2] + c[3]);
    return true;
}

#endif
