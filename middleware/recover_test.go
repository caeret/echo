// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestRecover(t *testing.T) {
	e := echo.New()
	buf := new(bytes.Buffer)
	//e.Logger.SetOutput(buf)
	e.SetLogger(&echo.SlogLogger{slog.New(slog.NewTextHandler(buf, nil))})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	h := Recover()(echo.HandlerFunc(func(c echo.Context) error {
		panic("test")
	}))
	err := h(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, buf.String(), "PANIC RECOVER")
}

func TestRecoverErrAbortHandler(t *testing.T) {
	e := echo.New()
	buf := new(bytes.Buffer)
	//e.Logger.SetOutput(buf)
	e.SetLogger(&echo.SlogLogger{slog.New(slog.NewTextHandler(buf, nil))})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	h := Recover()(echo.HandlerFunc(func(c echo.Context) error {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		r := recover()
		if r == nil {
			assert.Fail(t, "expecting `http.ErrAbortHandler`, got `nil`")
		} else {
			if err, ok := r.(error); ok {
				assert.ErrorIs(t, err, http.ErrAbortHandler)
			} else {
				assert.Fail(t, "not of error type")
			}
		}
	}()

	h(c)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, buf.String(), "PANIC RECOVER")
}

func TestRecoverWithConfig_LogErrorFunc(t *testing.T) {
	e := echo.New()
	//e.Logger.SetLevel(log.DEBUG)

	buf := new(bytes.Buffer)
	//e.Logger.SetOutput(buf)
	e.SetLogger(&echo.SlogLogger{slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	testError := errors.New("test")
	config := DefaultRecoverConfig
	config.LogErrorFunc = func(c echo.Context, err error, stack []byte) error {
		msg := fmt.Sprintf("[PANIC RECOVER] %v %s\n", err, stack)
		if errors.Is(err, testError) {
			c.Logger().Debug(msg)
		} else {
			c.Logger().Error(msg)
		}
		return err
	}

	t.Run("first branch case for LogErrorFunc", func(t *testing.T) {
		buf.Reset()
		h := RecoverWithConfig(config)(echo.HandlerFunc(func(c echo.Context) error {
			panic(testError)
		}))

		h(c)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)

		output := buf.String()
		assert.Contains(t, output, "PANIC RECOVER")
		assert.Contains(t, output, `DEBUG`)
	})

	t.Run("else branch case for LogErrorFunc", func(t *testing.T) {
		buf.Reset()
		h := RecoverWithConfig(config)(echo.HandlerFunc(func(c echo.Context) error {
			panic("other")
		}))

		h(c)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)

		output := buf.String()
		assert.Contains(t, output, "PANIC RECOVER")
		assert.Contains(t, output, `ERROR`)
	})
}

func TestRecoverWithDisabled_ErrorHandler(t *testing.T) {
	e := echo.New()
	buf := new(bytes.Buffer)
	//e.Logger.SetOutput(buf)
	e.SetLogger(&echo.SlogLogger{slog.New(slog.NewTextHandler(buf, nil))})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	config := DefaultRecoverConfig
	config.DisableErrorHandler = true
	h := RecoverWithConfig(config)(echo.HandlerFunc(func(c echo.Context) error {
		panic("test")
	}))
	err := h(c)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, buf.String(), "PANIC RECOVER")
	assert.EqualError(t, err, "test")
}
