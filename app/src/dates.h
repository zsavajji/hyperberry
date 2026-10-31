#ifndef DATES_H
#define DATES_H

#include <bb/utility/i18n/RelativeDateFormatter>
#include <QDate>
#include <QDateTime>
#include <QLocale>
#include <QString>

// BB10's labels are lower case in some languages ("oggi", "luglio"); a header starts upper case.
inline QString capitalized(QString s)
{
    if (!s.isEmpty())
        s[0] = s[0].toUpper();
    return s;
}

// A day's label in the phone's language: "Today" and "Yesterday" from BB10's formatter, the
// weekday for the rest of the week (the formatter dates days 4-6), else the formatter's date.
inline QString dayLabel(const QDateTime &when, const QDate &today, const QLocale &locale, bb::utility::i18n::RelativeDateFormatter &days)
{
    const QDate date = when.date();
    const int age = date.daysTo(today);
    return capitalized(age >= 2 && age < 7 ? locale.standaloneDayName(date.dayOfWeek())
                                           : days.format(when));
}

#endif
