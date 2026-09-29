## Summary
The GUI can show a randomly selected, subtle background image from
`[gui].background_image_dir`. It preserves aspect ratio at 12% opacity over black,
with a transparent editor background and dark-theme controls.

## Scope
Addresses #122 in a separate commit from Batch 9 within the combined publication PR. The directory is optional and absolute.
Only top-level regular JPEG/PNG files are candidates; unusable images fall through
to another candidate or the normal black background. Encoded size and pixel limits
bound decoding. The selected image stays fixed for the window lifetime.

## Validation
Config and GUI race tests; full make check; native arm64 build. Native GUI checks
used a temporary config/state/vault and verified multilingual editor text, success,
failure details and missing-directory black fallback. No live service was contacted.

## Review notes
No recursion, opacity setting, image posting or new dependency. The original issue's
Batch 7 deferral is preserved as history; the current request authorizes this work.
The issue remains open pending acceptance and merge.
