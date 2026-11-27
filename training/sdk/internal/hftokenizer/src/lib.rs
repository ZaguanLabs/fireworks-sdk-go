//! Isolated Hugging Face tokenizer and chat-template execution for the Go SDK.

mod chat;
use minijinja::Value as TemplateValue;
use serde::Deserialize;
use serde_json::{Value, json};
use std::cell::RefCell;
use std::ptr;
use tokenizers::Tokenizer;

thread_local! {
    static TOKENIZER: RefCell<Option<Tokenizer>> = const { RefCell::new(None) };
}

#[derive(Deserialize)]
struct EncodeRequest {
    text: String,
    add_special_tokens: bool,
}

#[derive(Deserialize)]
struct DecodeRequest {
    ids: Vec<u32>,
    skip_special_tokens: bool,
}

#[derive(Deserialize)]
struct RenderRequest {
    now: Option<String>,
    template: String,
    context: Value,
}

type GuestError = Box<dyn std::error::Error + Send + Sync>;

fn with_tokenizer(
    f: impl FnOnce(&Tokenizer) -> Result<Value, GuestError>,
) -> Result<Value, GuestError> {
    TOKENIZER.with_borrow(|slot| {
        f(slot
            .as_ref()
            .ok_or("load a tokenizer before encoding or decoding")?)
    })
}

fn process(op: u32, input: &[u8]) -> Result<Value, GuestError> {
    match op {
        1 => {
            tokenizers::utils::parallelism::set_parallelism(false);
            let tokenizer = Tokenizer::from_bytes(input)?;
            TOKENIZER.set(Some(tokenizer));
            Ok(Value::Null)
        }
        2 => {
            let req: EncodeRequest = serde_json::from_slice(input)?;
            with_tokenizer(|t| {
                Ok(json!(
                    t.encode(req.text.as_str(), req.add_special_tokens)?
                        .get_ids()
                ))
            })
        }
        3 => {
            let req: DecodeRequest = serde_json::from_slice(input)?;
            with_tokenizer(|t| Ok(Value::String(t.decode(&req.ids, req.skip_special_tokens)?)))
        }
        4 => {
            let req: RenderRequest = serde_json::from_slice(input)?;
            Ok(Value::String(chat::render(
                &req.template,
                TemplateValue::from(minijinja::value::Serde(req.context)),
                req.now,
            )?))
        }
        _ => Err(format!("unknown tokenizer operation {op}").into()),
    }
}

fn response(op: u32, input: &[u8]) -> Vec<u8> {
    match process(op, input) {
        Ok(result) => json!({"ok":result}),
        Err(err) => json!({"error":err.to_string()}),
    }
    .to_string()
    .into_bytes()
}

// The Go host creates one module instance per tokenizer and serializes its calls.
// Boxed slices have an exact allocation layout, recoverable using pointer and length.
#[unsafe(no_mangle)]
pub extern "C" fn alloc(len: u32) -> *mut u8 {
    Box::into_raw(vec![0; len as usize].into_boxed_slice()).cast()
}

/// Free an input or result buffer.
///
/// # Safety
/// The host must pass the pointer and original length of a live allocation returned
/// by `alloc` or `invoke` in this instance, at most once.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn dealloc(ptr: *mut u8, len: u32) {
    if !ptr.is_null() {
        // SAFETY: The ABI requires a live boxed byte slice with this exact length.
        drop(unsafe { Box::from_raw(ptr::slice_from_raw_parts_mut(ptr, len as usize)) });
    }
}

/// Return an owned response buffer packed as `(address << 32) | length`.
///
/// # Safety
/// The host must pass a readable input allocated by `alloc` and keep it unchanged until return.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn invoke(op: u32, ptr: *const u8, len: u32) -> u64 {
    // SAFETY: The host guarantees the lifetime, bounds and initialization of this input.
    let input = unsafe { std::slice::from_raw_parts(ptr, len as usize) };
    let result = response(op, input).into_boxed_slice();
    let len = result.len() as u32;
    let address = Box::into_raw(result).cast::<u8>() as usize;
    ((address as u64) << 32) | u64::from(len)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn wordlevel_tokenizer_encodes_and_decodes_and_failed_load_keeps_state() {
        let model = br#"{"version":"1.0","pre_tokenizer":{"type":"Whitespace"},"model":{"type":"WordLevel","vocab":{"[UNK]":0,"hi":1,"you":2},"unk_token":"[UNK]"}}"#;
        assert_eq!(process(1, model).unwrap(), json!(null));
        assert!(process(1, b"{}").is_err());
        assert_eq!(
            process(2, br#"{"text":"hi\nyou","add_special_tokens":false}"#).unwrap(),
            json!([1, 2])
        );
        assert_eq!(
            process(3, br#"{"ids":[1,2],"skip_special_tokens":false}"#).unwrap(),
            json!("hi you")
        );
    }
}
