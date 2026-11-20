// Run on the Mac: clang++ -std=c++11 -I app/src app/tests/sps_crop_test.cpp -o /tmp/sps_crop_test && /tmp/sps_crop_test
#include "SpsCrop.h"

#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <string>
#include <vector>

static std::vector<unsigned char> fromHex(const char *hex)
{
    std::vector<unsigned char> v;
    for (size_t i = 0; hex[i] && hex[i + 1]; i += 2) {
        const char two[3] = { hex[i], hex[i + 1], 0 };
        v.push_back((unsigned char)strtoul(two, 0, 16));
    }
    return v;
}

// The position of the rbsp_stop_one_bit: the last 1 bit.
static size_t stopPos(const std::vector<unsigned char> &d)
{
    size_t p = d.size() * 8;
    while (p > 0 && !spscrop::bitAt(d, p - 1))
        p--;
    return p - 1;
}

// A real x264 SPS NAL (header byte first, emulation prevention bytes in), cropped to 9:16.
// left, right: the frame_crop offsets expected after the crop; bottom: the bottom crop it had and keeps.
static void check(const char *hex, int codedW, int codedH, int wantW, int wantH, int left, int right, int bottom)
{
    const std::vector<unsigned char> in = fromHex(hex);
    SpsInfo a;
    assert(spsParse(&in[0], int(in.size()), &a));
    assert(a.width == codedW && a.height == codedH);

    std::vector<unsigned char> out;
    int w = 0, h = 0;
    assert(spsCropToAspect(&in[0], int(in.size()), 9, 16, &out, &w, &h));
    assert(w == wantW && h == wantH);
    assert(out[0] == in[0]);

    // The output parses again, with the same coded size, the same fields before the crop and the
    // crop the report says.
    SpsInfo b;
    assert(spsParse(&out[0], int(out.size()), &b));
    assert(b.width == a.width && b.height == a.height && b.cropUnitY == a.cropUnitY);
    assert(b.cropFlagPos == a.cropFlagPos);
    for (size_t p = 0; p < a.cropFlagPos; p++)
        assert(spscrop::bitAt(a.rbsp, p) == spscrop::bitAt(b.rbsp, p));
    assert(b.width - 2 * int(b.crop[0] + b.crop[1]) == w);
    assert(b.height - b.cropUnitY * int(b.crop[2] + b.crop[3]) == h);
    assert(int(b.crop[0]) == left && int(b.crop[1]) == right);
    assert(int(b.crop[3]) == bottom);
    // Everything after the crop is the original's (the VUI, the stop bit), bit for bit.
    const size_t tail = stopPos(a.rbsp) - a.restPos;
    assert(stopPos(b.rbsp) - b.restPos == tail);
    for (size_t p = 0; p < tail; p++)
        assert(spscrop::bitAt(a.rbsp, a.restPos + p) == spscrop::bitAt(b.rbsp, b.restPos + p));

    // Cropped already: a second crop to 9:16 changes nothing, and neither does 16:9 on the 16:9 original.
    std::vector<unsigned char> again(3, 0xAB);
    int w2 = -1, h2 = -1;
    assert(!spsCropToAspect(&out[0], int(out.size()), 9, 16, &again, &w2, &h2));
    assert(again.size() == 3 && again[0] == 0xAB && w2 == -1 && h2 == -1);

    char name[64];
    snprintf(name, sizeof name, "/tmp/sps_%dx%d.bin", w, h);
    FILE *f = fopen(name, "wb");
    assert(f);
    fwrite(&out[0], 1, out.size(), f);
    fclose(f);
    printf("%s: %dx%d coded -> %dx%d, crop l %u r %u t %u b %u, %d -> %d bytes\n", name, codedW, codedH, w, h,
           b.crop[0], b.crop[1], b.crop[2], b.crop[3], int(in.size()), int(out.size()));
}

int main()
{
    // 1280x720 baseline: 404x720, 219 off each side.
    const char *hd = "6742c01fd9005005bb0110000003001000000301e0f1832480";
    check(hd, 1280, 720, 404, 720, 219, 219, 0);
    // 640x480 baseline.
    check("6742c016d900a03db0110000030001000003001e0f162e48", 640, 480, 270, 480, 92, 93, 0);
    // 1920x1080 high, coded 1088 with a bottom crop of 4 units (8 rows): 606x1080, the bottom crop kept.
    check("67640028acd940780227e5c044000003000400000300783c60c658", 1920, 1088, 606, 1080, 328, 329, 4);

    // 16:9 on the 1280x720 original is already that shape; so is nothing to do with no aspect.
    const std::vector<unsigned char> in = fromHex(hd);
    std::vector<unsigned char> out;
    int w, h;
    assert(!spsCropToAspect(&in[0], int(in.size()), 16, 9, &out, &w, &h));
    assert(!spsCropToAspect(&in[0], int(in.size()), 0, 0, &out, &w, &h));
    // Not an SPS, and a truncated one.
    std::vector<unsigned char> pps = in;
    pps[0] = 0x68;
    assert(!spsCropToAspect(&pps[0], int(pps.size()), 9, 16, &out, &w, &h));
    assert(!spsCropToAspect(&in[0], 8, 9, 16, &out, &w, &h));
    printf("ok\n");
    return 0;
}
