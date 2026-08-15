CC ?= cc
CFLAGS ?= -std=c11 -Wall -Wextra -O2
LDLIBS = -lpthread

# The default build supports unix + plaintext tcp:// with zero deps
# (libc + pthreads). TLS (tls:// endpoints) is opt-in and needs OpenSSL:
#   make KT_TLS=1
#
# OpenSSL discovery: prefer pkg-config (Linux, and macOS once its
# pkgconfig is on PKG_CONFIG_PATH); otherwise fall back to Homebrew's
# keg-only openssl@3 prefix on macOS; otherwise assume it is on the
# default search path.
ifdef KT_TLS
CFLAGS += -DKT_TLS
OPENSSL_CFLAGS := $(shell pkg-config --cflags openssl 2>/dev/null)
OPENSSL_LIBS := $(shell pkg-config --libs openssl 2>/dev/null)
ifeq ($(strip $(OPENSSL_LIBS)),)
  OPENSSL_PREFIX := $(shell brew --prefix openssl@3 2>/dev/null)
  ifneq ($(strip $(OPENSSL_PREFIX)),)
    OPENSSL_CFLAGS := -I$(OPENSSL_PREFIX)/include
    OPENSSL_LIBS := -L$(OPENSSL_PREFIX)/lib -lssl -lcrypto
  else
    OPENSSL_LIBS := -lssl -lcrypto
  endif
endif
CFLAGS += $(OPENSSL_CFLAGS)
LDLIBS += $(OPENSSL_LIBS)
endif

.PHONY: all clean test
all: demoapp counter

# demoapp bridges its terminals to real client-side PTYs (ptydriver.c),
# which needs forkpty from libutil on Linux (harmless elsewhere).
demoapp: demoapp.c kittytk.c scripts.c ptydriver.c kittytk.h scripts.h ptydriver.h
	$(CC) $(CFLAGS) -o $@ demoapp.c kittytk.c scripts.c ptydriver.c $(LDLIBS) -lutil

# The smallest useful app: a label + a button that increments it.
counter: counter.c kittytk.c kittytk.h
	$(CC) $(CFLAGS) -o $@ counter.c kittytk.c $(LDLIBS)

# A window whose sole child is a mew editor control filling it. Requires
# a host built with -tags mew (the "editor" trinket type). The C client
# itself is plain - only the host needs the tag.
mewdemo: mewdemo.c kittytk.c kittytk.h
	$(CC) $(CFLAGS) -o $@ mewdemo.c kittytk.c $(LDLIBS)

# Interop smoke binaries (also driven by the Go tests in c/interop).
interop_smoke: interop_smoke.c kittytk.c kittytk.h
	$(CC) $(CFLAGS) -o $@ interop_smoke.c kittytk.c $(LDLIBS)

demoapp_smoke: demoapp_smoke.c kittytk.c scripts.c kittytk.h scripts.h
	$(CC) $(CFLAGS) -o $@ demoapp_smoke.c kittytk.c scripts.c $(LDLIBS)

# Interop against a REAL Go host (bidirectional events + full demo build).
test:
	cd .. && go test ./c/interop/

clean:
	rm -f demoapp counter mewdemo interop_smoke demoapp_smoke
