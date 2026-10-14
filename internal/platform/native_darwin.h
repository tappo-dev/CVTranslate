#include <stdint.h>
#include <stddef.h>

void *cv_create(void);
void cv_close(void *state);
int cv_trusted(void);
int cv_capture(void *state, char **text, uint64_t *version, uint64_t *token);
int cv_validate(void *state, uint64_t token, uint64_t version);
int cv_released(void);
int cv_write(uint64_t version, const char *text, uint64_t *written);
int cv_dispatch(void *state, uint64_t token, uint64_t version);
int cv_copy_permitted(void);
int cv_copy_run(uintptr_t handle);
void cv_copy_stop(void *listener);
void cv_copy_dispose(void *listener);
void cv_copy_snapshot(uintptr_t *foreground, uint64_t *version);
int cv_validate_clipboard(uint64_t version);
int cv_dispatch_direct(uint64_t version, uintptr_t context);
void cv_go_copy_event(uintptr_t handle, int kind, int key, int commandOnly, int autoRepeat, int injected);
void cv_go_copy_ready(uintptr_t handle, void *listener, int result);
int cv_go_paste_cancelled(uintptr_t context);
int cv_credential_get(const char *service, const char *account, void **value, size_t *length);
int cv_credential_set(const char *service, const char *account, const void *value, size_t length);
int cv_credential_delete(const char *service, const char *account);
