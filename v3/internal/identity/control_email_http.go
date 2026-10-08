package identity

import "net/http"

func (c *Control) emailVerificationHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.sameOrigin(r) {
		c.reply(w, nil, ErrForbidden)
		return
	}
	if !c.allowLogin(w, r) {
		return
	}
	var uid *int64
	if requestToken(r) != "" {
		u, err := c.AuthenticateRequest(r)
		if err != nil {
			c.reply(w, nil, err)
			return
		}
		uid = &u.ID
	}
	c.reply(w, nil, c.SendEmailVerification(r.Context(), r.URL.Query().Get("email"), uid))
}

func (c *Control) verifyEmailHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	if !c.allowLogin(w, r) {
		return
	}
	var in struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := decodeControl(w, r, &in); err != nil {
		c.reply(w, nil, err)
		return
	}
	data, err := c.VerifyEmail(r.Context(), u.ID, in.Email, in.Code)
	c.reply(w, data, err)
}

func (c *Control) passwordResetEmailHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.sameOrigin(r) {
		c.reply(w, nil, ErrForbidden)
		return
	}
	if !c.allowLogin(w, r) {
		return
	}
	c.reply(w, nil, c.SendPasswordReset(r.Context(), r.URL.Query().Get("email")))
}

func (c *Control) passwordResetHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.allowLogin(w, r) {
		return
	}
	var in struct {
		Email    string `json:"email"`
		Token    string `json:"token"`
		Password string `json:"password,omitempty"`
	}
	if err := decodeControl(w, r, &in); err != nil {
		c.reply(w, nil, err)
		return
	}
	generated, err := c.ResetPassword(r.Context(), in.Email, in.Token, in.Password)
	var data any
	if generated != "" {
		data = generated
	}
	c.reply(w, data, err)
}
