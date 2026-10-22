package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"domainradar/internal/domain"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type recordingAlertChannel struct {
	notifications chan Notification
}

func (c *recordingAlertChannel) Send(_ context.Context, n *Notification) error {
	c.notifications <- *n
	return nil
}
func (c *recordingAlertChannel) TestConnection(context.Context, *ChannelConfig) error { return nil }
func (c *recordingAlertChannel) ChannelType() string                                  { return "webhook" }

func TestAlertDispatcher_DomainAndAccountScopes(t *testing.T) {
	for _, accountLevel := range []bool{false, true} {
		name := "domain"
		if accountLevel {
			name = "account"
		}
		t.Run(name, func(t *testing.T) {
			ts := newHandlerTestSetup(t)
			conn, err := ts.db.DB()
			require.NoError(t, err)
			conn.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, conn.Close()) })
			require.NoError(t, ts.db.AutoMigrate(&domain.Alert{}, &domain.NotificationLog{}))
			require.NoError(t, ts.db.Exec("PRAGMA foreign_keys = ON").Error)
			dom := domain.NormalizedDomain{DomainName: "first-domain.test"}
			require.NoError(t, ts.db.Create(&dom).Error)
			ch := domain.NotificationChannel{ChannelType: "webhook", Name: "recording", Status: "active"}
			require.NoError(t, ts.db.Create(&ch).Error)
			alert := domain.Alert{
				DomainID: &dom.ID, AlertType: "expiration", Severity: "warning",
				Message: "test notification", GeneratedAt: time.Now(),
			}
			expectedDomain := dom.DomainName
			if accountLevel {
				config := domain.RegistrarConfig{RegistrarType: "cloudflare"}
				require.NoError(t, ts.db.Create(&config).Error)
				account := domain.RegistrarAccount{RegistrarConfigID: config.ID, AccountName: "recording"}
				require.NoError(t, ts.db.Create(&account).Error)
				alert.DomainID = nil
				alert.RegistrarAccountID = &account.ID
				alert.AlertType = "sync_failure"
				expectedDomain = ""
			}
			require.NoError(t, ts.db.Create(&alert).Error)
			recording := &recordingAlertChannel{notifications: make(chan Notification, 1)}
			ts.channelRegistry.Register("webhook", func(*ChannelConfig) NotificationChannel { return recording })
			dispatcher := NewAlertDispatcher(ts.db, ts.cryptoService, ts.channelRegistry, zap.NewNop())
			dispatcher.DispatchAlert(&alert)
			select {
			case n := <-recording.notifications:
				require.Equal(t, alert.ID, n.AlertID)
				require.Equal(t, alert.AlertType, n.AlertType)
				require.Equal(t, expectedDomain, n.DomainName)
				require.Equal(t, alert.Message, n.Message)
			case <-time.After(time.Second):
				t.Fatal("notification was not dispatched")
			}
			require.Eventually(t, func() bool {
				var count int64
				err := ts.db.Model(&domain.NotificationLog{}).Where("alert_id = ? AND status = ?", alert.ID, "sent").Count(&count).Error
				return err == nil && count == 1
			}, time.Second, 10*time.Millisecond)
			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/notifications/channels/"+strconv.Itoa(int(ch.ID))+"/logs", nil)
			ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(int(ch.ID))}}
			ts.handler.ListChannelLogs(ctx)
			require.Equal(t, http.StatusOK, w.Code)
			var logs struct {
				Data []struct {
					DomainName string `json:"domain_name"`
					AlertType  string `json:"alert_type"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &logs))
			require.Len(t, logs.Data, 1)
			require.Equal(t, expectedDomain, logs.Data[0].DomainName)
			require.Equal(t, alert.AlertType, logs.Data[0].AlertType)
		})
	}
}
