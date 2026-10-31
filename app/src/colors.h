#ifndef COLORS_H
#define COLORS_H

#include <QString>

// One of n colours for a key (a jid, a sender), the same on every run. Not qHash: Qt 4's string
// hash keeps the last character in its low bits, so every "…@s.whatsapp.net" got one colour
// (Q10, 2026-10-31). FNV-1a spreads them.
inline const char *pickColor(const QString &key, const char *const *colors, int n)
{
    uint h = 2166136261u;
    for (int i = 0; i < key.size(); ++i) {
        h ^= key.at(i).unicode();
        h *= 16777619u;
    }
    return colors[h % n];
}

#endif
