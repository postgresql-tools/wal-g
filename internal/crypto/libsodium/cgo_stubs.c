// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

// Weak stub definitions for Go 1.25 cgo DWARF analysis under -mod=vendor.
// cgo needs compiled C source with function bodies to produce DWARF type
// information. These weak stubs provide that; the linker prefers the real
// implementations from libsodium.a (baked in by link_libsodium.sh).
#include "gen/walg_config.h"

int sodium_init() __attribute__((weak));
int sodium_init() { return 0; }

int walg_secretstream_init_push(walg_secretstream_state *state, unsigned char *header, const unsigned char *key) __attribute__((weak));
int walg_secretstream_init_push(walg_secretstream_state *state, unsigned char *header, const unsigned char *key) { return 0; }

int walg_secretstream_init_pull(walg_secretstream_state *state, const unsigned char *header, const unsigned char *key) __attribute__((weak));
int walg_secretstream_init_pull(walg_secretstream_state *state, const unsigned char *header, const unsigned char *key) { return 0; }

int walg_secretstream_push(walg_secretstream_state *state, unsigned char *out, unsigned long long *out_len, const unsigned char *in, unsigned long long in_len, const unsigned char *ad, unsigned long long ad_len, unsigned char tag) __attribute__((weak));
int walg_secretstream_push(walg_secretstream_state *state, unsigned char *out, unsigned long long *out_len, const unsigned char *in, unsigned long long in_len, const unsigned char *ad, unsigned long long ad_len, unsigned char tag) { return 0; }

int walg_secretstream_pull(walg_secretstream_state *state, unsigned char *out, unsigned long long *out_len, unsigned char *tag, const unsigned char *in, unsigned long long in_len, const unsigned char *ad, unsigned long long ad_len) __attribute__((weak));
int walg_secretstream_pull(walg_secretstream_state *state, unsigned char *out, unsigned long long *out_len, unsigned char *tag, const unsigned char *in, unsigned long long in_len, const unsigned char *ad, unsigned long long ad_len) { return 0; }
