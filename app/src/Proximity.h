#ifndef PROXIMITY_H
#define PROXIMITY_H

#include <bb/AbstractBpsEventHandler>
#include <QObject>

// The proximity sensor, for the call screen: when the phone is at the ear the screen goes black
// and takes no touch. On while someone needs it: acquire() and release() count, the sensor's events
// are requested on the first acquire and stopped on the last release. The screen also stays awake
// while held, because an idle lock during a call would minimise the app and drop the audio (the
// same reason as while recording, step 4.4b). Lives on the UI thread, where BPS delivers.
class Proximity : public QObject, public bb::AbstractBpsEventHandler
{
    Q_OBJECT
    // True while an object is close to the sensor; false when released or the sensor is far.
    Q_PROPERTY(bool near READ isNear NOTIFY nearChanged)
public:
    explicit Proximity(QObject *parent = 0);
    ~Proximity();

    bool isNear() const { return m_near; }
    Q_INVOKABLE void acquire();
    Q_INVOKABLE void release();

    void event(bps_event_t *event);

signals:
    void nearChanged();

private:
    void setNear(bool on);

    int m_users;
    bool m_near;
};

#endif
