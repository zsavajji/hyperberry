# HyperBerry's UI, a plain executable as the bar's Qnx/Elf entry point. Built and packaged by
# scripts/build-app.sh, out of tree in build/ui.
TEMPLATE = app
TARGET = hyperberry
CONFIG += qt warn_on release
QT += declarative network sql gui
# QtGui links anyway (QtDeclarative needs it); QImage and QImageReader make the photo previews and the
# thumbnail sent with a photo (step 6.3), since the engine decodes no picture.
LIBS += -lbbcascades -lbbdata -lbbsystem -lbbplatform -lbb -lbbutilityi18n -lslog2
QMAKE_LFLAGS += -Wl,-rpath-link,$${QNX_DIR}/$${QNX_CPUDIR}/usr/lib/qt4/lib
QMAKE_CXXFLAGS += -Wno-psabi

SOURCES += src/main.cpp src/EngineLink.cpp src/ChatListModel.cpp src/EmojiModel.cpp src/MessageModel.cpp src/ChatController.cpp src/ContactInfo.cpp src/ContactPicker.cpp src/MediaData.cpp src/MediaOpener.cpp src/QrImage.cpp src/Waveform.cpp src/OpenRequests.cpp src/Profile.cpp src/Settings.cpp $$PWD/../lib/qrcodegen/qrcodegen.c
HEADERS += src/EngineLink.h src/ChatListModel.h src/EmojiModel.h src/emojiTable.h src/MessageModel.h src/ChatController.h src/ContactInfo.h src/ContactPicker.h src/MediaData.h src/MediaOpener.h src/Moves.h src/dates.h src/colors.h src/QrImage.h src/Waveform.h src/OpenRequests.h src/Profile.h src/Settings.h src/sizes.h

DESTDIR = app
OBJECTS_DIR = obj
MOC_DIR = moc

# qrcodegen is C99, fetched by scripts/fetch-qrcodegen.sh.
INCLUDEPATH += $$PWD/../lib/qrcodegen
QMAKE_CFLAGS += -std=c99

# lupdate reads the QML too; scripts/build-app.sh turns the .ts into the .qm the bar ships.
CODECFORTR = UTF-8
CODECFORSRC = UTF-8
TRANSLATIONS = translations/hyperberry_it.ts
lupdate_only {
    SOURCES += assets/*.qml assets/errors.js
}
