# HyperBerry's UI, a plain executable as the bar's Qnx/Elf entry point. Built and packaged by
# scripts/build-app.sh, out of tree in build/ui.
TEMPLATE = app
TARGET = hyperberry
CONFIG += qt warn_on release
QT += declarative network sql
# No QtGui: bb::ImageData and bb::utility::ImageConverter instead of QImage.
QT -= gui
LIBS += -lbbcascades -lbbdata -lbbsystem -lbb
QMAKE_LFLAGS += -Wl,-rpath-link,$${QNX_DIR}/$${QNX_CPUDIR}/usr/lib/qt4/lib
QMAKE_CXXFLAGS += -Wno-psabi

SOURCES += src/main.cpp src/gates.cpp
HEADERS += src/gates.h

DESTDIR = app
OBJECTS_DIR = obj
MOC_DIR = moc
