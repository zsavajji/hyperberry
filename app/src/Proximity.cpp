#include "Proximity.h"

#include <bb/cascades/Application>
#include <bb/cascades/Window>
#include <bps/bps.h>
#include <bps/sensor.h>
#include <errno.h>
#include <QDebug>

Proximity::Proximity(QObject *parent)
    : QObject(parent)
    , m_users(0)
    , m_near(false)
{
}

Proximity::~Proximity()
{
    if (m_users > 0) {
        m_users = 1;
        release();
    }
}

static void keepAwake(bool on)
{
    bb::cascades::Application *app = bb::cascades::Application::instance();
    if (app && app->mainWindow())
        app->mainWindow()->setScreenIdleMode(on ? bb::cascades::ScreenIdleMode::KeepAwake
                                                : bb::cascades::ScreenIdleMode::Normal);
}

void Proximity::acquire()
{
    if (m_users++ > 0)
        return;
    keepAwake(true);
    // Cascades has started BPS already; the sensor domain is delivered to this handler.
    subscribe(sensor_get_domain());
    if (sensor_request_events(SENSOR_TYPE_PROXIMITY) != BPS_SUCCESS)
        qWarning("proximity: sensor refused, errno %d", errno);
}

void Proximity::release()
{
    if (m_users <= 0 || --m_users > 0)
        return;
    sensor_stop_events(SENSOR_TYPE_PROXIMITY);
    unsubscribe(sensor_get_domain());
    keepAwake(false);
    setNear(false);
}

void Proximity::event(bps_event_t *event)
{
    if (bps_event_get_domain(event) != sensor_get_domain()
        || bps_event_get_code(event) != SENSOR_PROXIMITY_READING)
        return;
    // 0.0 is close, 1.0 far.
    setNear(sensor_event_get_proximity(event) < 0.5f);
}

void Proximity::setNear(bool on)
{
    if (m_near == on)
        return;
    m_near = on;
    emit nearChanged();
}
