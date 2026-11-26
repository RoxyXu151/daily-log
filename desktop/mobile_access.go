package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

type mobileAccessOptions struct {
	listen, certificate, key, assets string
}

type mobileAccessSession struct {
	url, code string
	issueCode func() (string, error)
	stop      func()
	done      <-chan error
	stopping  bool
}

// mobileHeading is the line a pairing card opens with: the thing being
// paired, what it needs, and whether it is live. The card already gives the
// surface, so the row only lays the words out — no box drawn around them.
func (s *screen) mobileHeading(c *ui.Context, detail string, live bool, key string) {
	p := paletteFor(c.Theme())
	wash, iconColor := p.hover, p.muted
	status, tone := "未启用", toneNeutral
	if live {
		wash, iconColor = p.accent.Alpha(0.13), p.accent
		status, tone = "已启用", toneGood
	}
	ui.Row(c).Wrap().Gap(spaceL).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(40, 40).Shrink(0).Radius(radiusTile).Center().Background(wash).Children(func() {
			ui.Icon(c, iconPhone).Size(18, 18).TextColor(iconColor)
		})
		ui.Column(c).Gap(spaceXS).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, "Android 浏览器访问").FontSize(sizeCardTitle).FontWeight(weightBold).
				LetterSpacing(trackingCard)
			ui.Text(c, detail).FontSize(sizeMeta).TextColor(p.muted)
		})
		statusPill(c, status, tone, key)
	})
}

func (s *screen) mobileAccess(c *ui.Context) {
	p := paletteFor(c.Theme())
	pageHeader(c, "DEVICE", "手机配对", "用手机浏览器打开工作台，随时记录和复核。")
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(spaceL).Children(func() {
			session := s.mobileSession
			if session != nil {
				card(c.Key("mobile-session"), "", func() {
					s.mobileHeading(c, "局域网 HTTPS 会话", true, "mobile-enabled")
					divider(c)
					sectionLabel(c, "访问地址")
					sunken(c, func() {
						ui.Text(c, session.url).FontSize(sizeField).FontWeight(weightSemibold).Selectable()
					})
					sectionLabel(c, "一次性配对码")
					ui.Row(c).Wrap().Gap(spaceL).AlignItems(ui.Center).Children(func() {
						// The code is the one thing here meant to be read at a
						// glance, so it lifts on a tile of its own rather than
						// being ringed in accent: the digits carry the weight.
						tile(c, func() {
							ui.Text(c, session.code).FontSize(sizeFigure).FontWeight(weightBold).
								LetterSpacing(trackingLabel).FontFeatures("tnum").
								TextColor(p.accent).Selectable()
						})
						statusPill(c, "10 分钟内有效", toneWarn, "mobile-code-ttl")
					})
					s.mobileNote(c, "在手机浏览器打开此地址，输入当前一次性配对码。配对码有效期为 10 分钟。")
					s.mobileNote(c, "成功配对后，会话有效期为 12 小时。下一次成功配对会替换当前会话。")
					divider(c)
					actionRow(c, func() {
						if pillButton(c, "new-code", "生成新配对码", iconRefresh, true, session.stopping).Clicked() {
							code, err := session.issueCode()
							s.failed = err != nil
							if err != nil {
								s.message = err.Error()
							} else {
								session.code = code
								s.notify("已生成新配对码")
							}
						}
						if pillButton(c, "stop-mobile", "停止手机访问", nil, false, session.stopping).Clicked() {
							session.stopping = true
							s.message = "正在停止手机访问…"
							session.stop()
						}
					})
				})
				return
			}
			form := &s.mobileOptions
			card(c.Key("mobile-setup"), "", func() {
				s.mobileHeading(c, "需要与地址匹配的 HTTPS 证书", false, "mobile-disabled")
				s.mobileNote(c, "手机访问需要可达的主机地址，以及地址匹配且受手机信任的 HTTPS 证书。")
				divider(c)
				sectionLabel(c, "连接设置")
				ui.Column(c).Gap(spaceM).Children(func() {
					ui.Column(c.Key("listen-group")).Gap(spaceXS).Children(func() {
						ui.Text(c, "主机地址和端口").FontSize(sizeSmall).FontWeight(weightSemibold).
							LetterSpacing(trackingLabel).TextColor(p.muted)
						ui.TextInput(c.Key("listen"), &form.listen).Label("主机地址和端口").
							Placeholder("192.168.1.20:8443").Disabled(s.busy)
					})
					s.mobilePath(c.Key("certificate"), "HTTPS 证书", &form.certificate, false)
					s.mobilePath(c.Key("private-key"), "HTTPS 私钥", &form.key, false)
					s.mobilePath(c.Key("assets"), "浏览器资源目录", &form.assets, true)
				})
				ready := strings.TrimSpace(form.listen) != "" && strings.TrimSpace(form.certificate) != "" &&
					strings.TrimSpace(form.key) != "" && strings.TrimSpace(form.assets) != ""
				divider(c)
				actionRow(c, func() {
					if pillButton(c, "enable-mobile", "启用手机访问", iconArrow, true, s.busy || !ready).Clicked() {
						s.enableMobileAccess()
					}
					if !ready {
						ui.Text(c, "填写全部连接设置后即可启用。").FontSize(sizeMeta).TextColor(p.muted)
					}
				})
			})
		})
	})
}

// mobileNote is a line of instruction under the thing it explains: the body
// step, kept quiet so the address and the code stay the loudest things here.
func (s *screen) mobileNote(c *ui.Context, message string) {
	ui.Text(c, message).FontSize(sizeBody).TextColor(paletteFor(c.Theme()).muted).
		LineHeight(leadBody)
}

func (s *screen) mobilePath(c *ui.Context, label string, value *string, directory bool) {
	ui.Column(c).Gap(spaceXS).Children(func() {
		ui.Text(c, label).FontSize(sizeSmall).FontWeight(weightSemibold).
			LetterSpacing(trackingLabel).TextColor(paletteFor(c.Theme()).muted)
		// The field keeps the room and the button hugs its words, so neither
		// stretches into a bar.
		ui.Row(c).Gap(spaceS).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("path"), value).Label(label).Grow(1).MinWidth(0).Disabled(s.busy)
			if s.pickMobilePath != nil &&
				pillButton(c, "choose-"+label, "选择", nil, false, s.busy).Clicked() {
				s.perform("打开文件选择…", func() (func(), error) {
					path, err := s.pickMobilePath(label, directory)
					return func() {
						if err == nil && path != "" {
							*value = path
						}
					}, err
				})
			}
		})
	})
}

func (s *screen) enableMobileAccess() {
	options := s.mobileOptions
	s.perform("正在启用手机访问…", func() (func(), error) {
		session, err := s.startMobile(options)
		if err != nil {
			return nil, err
		}
		return func() {
			s.mobileSession = session
			go s.watchMobileSession(session)
		}, nil
	})
}

func (s *screen) watchMobileSession(session *mobileAccessSession) {
	select {
	case err := <-session.done:
		if s.ctx.Err() != nil {
			return
		}
		s.dispatch(func() {
			if s.mobileSession == session {
				s.mobileSession = nil
				s.failed, s.message = err != nil, "手机访问已停止"
				if err != nil {
					s.message = err.Error()
				}
			}
		})
	case <-s.ctx.Done():
	}
}
