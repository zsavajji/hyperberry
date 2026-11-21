.pragma library
// The languages a message or a chat can be translated into (TranslatePicker and TranslateDialog's "Translate to"):
// those every service HyperBerry offers takes with the same code (the user, 2026-11-21: "the lowest possible number
// that gives no error on all services"): libretranslate.com's 51, all on DeepL's list and Google's, one code each:
// zh (LibreTranslate's alias of zh-Hans, DeepL's ZH, Google's zh), pt (DeepL gets PT-PT); zh-Hant, nb and pt-BR are
// left out, since no one code is sure on all three (Google's keyed API documents zh-TW and no, which LibreTranslate
// and DeepL refuse). The code, then the language's name in its own language (never translated); Latin script first,
// alphabetical, then the other scripts. A function, not a variable: the list stays a plain JS array when it crosses
// into QML properties.
function list() {
    return [["az", "Azərbaycan"], ["id", "Bahasa Indonesia"], ["ms", "Bahasa Melayu"], ["ca", "Català"],
        ["cs", "Čeština"], ["da", "Dansk"], ["de", "Deutsch"], ["et", "Eesti"],
        ["en", "English"], ["es", "Español"], ["eo", "Esperanto"], ["eu", "Euskara"],
        ["tl", "Filipino"], ["fr", "Français"], ["ga", "Gaeilge"], ["gl", "Galego"],
        ["it", "Italiano"], ["sw", "Kiswahili"], ["lv", "Latviešu"], ["lt", "Lietuvių"],
        ["hu", "Magyar"], ["nl", "Nederlands"], ["pl", "Polski"], ["pt", "Português"],
        ["ro", "Română"], ["sq", "Shqip"], ["sk", "Slovenčina"], ["sl", "Slovenščina"],
        ["fi", "Suomi"], ["sv", "Svenska"], ["vi", "Tiếng Việt"], ["tr", "Türkçe"],
        ["el", "Ελληνικά"],
        ["bg", "Български"], ["ky", "Кыргызча"], ["ru", "Русский"], ["sr", "Српски"],
        ["uk", "Українська"],
        ["he", "עברית"], ["ur", "اردو"], ["ar", "العربية"], ["fa", "فارسی"],
        ["hi", "हिन्दी"], ["bn", "বাংলা"],
        ["th", "ไทย"],
        ["zh", "中文（简体）"], ["ja", "日本語"], ["ko", "한국어"]]
}

// The name list() gives code, "" for a code it hasn't.
function name(code) {
    var all = list()
    for (var i = 0; i < all.length; i++) {
        if (all[i][0] == code) {
            return all[i][1]
        }
    }
    return ""
}
