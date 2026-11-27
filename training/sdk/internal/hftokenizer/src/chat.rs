//! The transformers chat-template environment uses Python-compatible string and map
//! methods and json.dumps with Unicode enabled and spaces after delimiters.
use chrono::{DateTime, format::StrftimeItems};
use minijinja::machinery::{Token, tokenize};
use minijinja::{Environment, Error, ErrorKind, Value, syntax::SyntaxConfig, value::Kwargs};
use serde_json::Value as Json;
use std::{borrow::Cow, fmt::Write};

fn invalid(msg: impl Into<String>) -> Error {
    Error::new(ErrorKind::InvalidOperation, msg.into())
}

struct JsonFormat {
    indent: Option<String>,
    comma: String,
    colon: String,
    ascii: bool,
}

impl JsonFormat {
    fn line(&self, out: &mut String, depth: usize) {
        if let Some(indent) = &self.indent {
            out.push('\n');
            for _ in 0..depth {
                out.push_str(indent)
            }
        }
    }

    fn string(&self, out: &mut String, s: &str) -> Result<(), Error> {
        let escaped = serde_json::to_string(s).map_err(|e| invalid(e.to_string()))?;
        if self.ascii {
            for ch in escaped.chars() {
                if (' '..='~').contains(&ch) {
                    out.push(ch)
                } else {
                    for unit in ch.encode_utf16(&mut [0; 2]) {
                        write!(out, "\\u{unit:04x}").map_err(|e| invalid(e.to_string()))?;
                    }
                }
            }
        } else {
            out.push_str(&escaped)
        }
        Ok(())
    }

    fn value(&self, out: &mut String, val: &Json, depth: usize) -> Result<(), Error> {
        match val {
            Json::Object(items) => {
                out.push('{');
                for (i, (k, v)) in items.iter().enumerate() {
                    if i > 0 {
                        out.push_str(&self.comma)
                    }
                    self.line(out, depth + 1);
                    self.string(out, k)?;
                    out.push_str(&self.colon);
                    self.value(out, v, depth + 1)?;
                }
                if !items.is_empty() {
                    self.line(out, depth)
                }
                out.push('}');
            }
            Json::Array(items) => {
                out.push('[');
                for (i, item) in items.iter().enumerate() {
                    if i > 0 {
                        out.push_str(&self.comma)
                    }
                    self.line(out, depth + 1);
                    self.value(out, item, depth + 1)?;
                }
                if !items.is_empty() {
                    self.line(out, depth)
                }
                out.push(']');
            }
            Json::String(s) => self.string(out, s)?,
            Json::Number(n) if n.is_f64() => {
                let f = n
                    .as_f64()
                    .ok_or_else(|| invalid("invalid floating-point number"))?;
                let value = format!("{f:?}");
                if let Some((mantissa, power)) = value.split_once('e') {
                    let exp = power.parse::<i16>().map_err(|e| invalid(e.to_string()))?;
                    write!(out, "{mantissa}e{exp:+03}").map_err(|e| invalid(e.to_string()))?;
                } else {
                    out.push_str(&value)
                }
            }
            _ => out.push_str(&val.to_string()),
        }
        Ok(())
    }
}

fn tojson(
    value: Value,
    args: minijinja::value::Rest<minijinja::value::ValueOrKwargs>,
) -> Result<String, Error> {
    let args = args.into_values();
    let (positional, kwargs): (&[Value], Kwargs) = minijinja::value::from_args(&args)?;
    let (positional_ascii, positional_indent, positional_sep, positional_sort): (
        Option<bool>,
        Option<Value>,
        Option<Vec<String>>,
        Option<bool>,
    ) = minijinja::value::from_args(positional)?;
    for (i, k) in ["ensure_ascii", "indent", "separators", "sort_keys"]
        .iter()
        .enumerate()
    {
        if i < positional.len() && kwargs.has(k) {
            return Err(invalid(format!("duplicate JSON option {k}")));
        }
    }
    let ascii = positional_ascii
        .or(kwargs.get("ensure_ascii")?)
        .unwrap_or(false);
    let indent = positional_indent.or(kwargs.get("indent")?);
    let indent = match indent {
        Some(v) if !v.is_none() => Some(if let Some(s) = v.as_str() {
            s.to_owned()
        } else {
            let n = i64::try_from(v).map_err(|_| invalid("indent must be a string or integer"))?;
            if n > 65536 {
                return Err(invalid("JSON indentation exceeds memory limit"));
            }
            " ".repeat(n.max(0) as usize)
        }),
        _ => None,
    };
    let sep = positional_sep.or(kwargs.get("separators")?);
    let sort = positional_sort
        .or(kwargs.get("sort_keys")?)
        .unwrap_or(false);
    kwargs.assert_all_used()?;
    let (comma, colon) = match sep {
        Some(s) if s.len() == 2 => (s[0].clone(), s[1].clone()),
        Some(_) => return Err(invalid("JSON separators must be a pair of strings")),
        None => (
            (if indent.is_some() { "," } else { ", " }).into(),
            ": ".into(),
        ),
    };
    let mut json = serde_json::to_value(value).map_err(|e| invalid(e.to_string()))?;
    if sort {
        json.sort_all_objects()
    }
    let mut output = String::new();
    JsonFormat {
        indent,
        comma,
        colon,
        ascii,
    }
    .value(&mut output, &json, 0)?;
    Ok(output)
}

// Transformers' generation blocks mark assistant-token spans. For text rendering
// they leave their body intact. Use lexer offsets so quoted strings, raw blocks
// and comments containing the tag are preserved.
fn generation_blocks(template: &str) -> Result<Cow<'_, str>, Error> {
    if !template.contains("generation") {
        return Ok(Cow::Borrowed(template));
    }
    let mut replacements = Vec::new();
    let mut block = false;
    for item in tokenize(template, false, SyntaxConfig::default()) {
        let (token, span) = item?;
        if block {
            let word = match token {
                Token::Ident("generation") => Some("if true"),
                Token::Ident("endgeneration") => Some("endif"),
                _ => None,
            };
            if let Some(new) = word {
                replacements.push((span.start_offset as usize, span.end_offset as usize, new));
            }
        }
        block = matches!(token, Token::BlockStart);
    }
    if replacements.is_empty() {
        return Ok(Cow::Borrowed(template));
    }
    let mut out = String::with_capacity(template.len());
    let mut start = 0;
    for (a, b, s) in replacements {
        out.push_str(&template[start..a]);
        out.push_str(s);
        start = b;
    }
    out.push_str(&template[start..]);
    Ok(Cow::Owned(out))
}

pub(super) fn render(template: &str, context: Value, now: Option<String>) -> Result<String, Error> {
    let mut env = Environment::new();
    env.set_syntax(
        SyntaxConfig::builder()
            .trim_blocks(true)
            .lstrip_blocks(true)
            .build()?,
    );
    env.set_fuel(Some(100_000_000));
    env.set_unknown_method_callback(minijinja_contrib::pycompat::unknown_method_callback);
    env.add_filter("tojson", tojson);
    env.add_function(
        "raise_exception",
        |message: String| -> Result<Value, Error> { Err(invalid(message)) },
    );
    env.add_function(
        "strftime_now",
        move |format: String| -> Result<String, Error> {
            let now = now
                .as_deref()
                .ok_or_else(|| invalid("the host did not provide a render timestamp"))?;
            let instant = DateTime::parse_from_rfc3339(now).map_err(|e| invalid(e.to_string()))?;
            // datetime.now() in transformers is local and timezone-naive. Python
            // uses six digits for %f; chrono uses nanoseconds by default.
            let mut directives = String::with_capacity(format.len());
            let mut chars = format.chars();
            while let Some(ch) = chars.next() {
                if ch != '%' {
                    directives.push(ch);
                    continue;
                }
                match chars.next() {
                    Some('f') => directives.push_str("%6f"),
                    Some('z' | 'Z') => {}
                    Some(code) => {
                        directives.push('%');
                        directives.push(code)
                    }
                    None => directives.push('%'),
                }
            }
            let items = StrftimeItems::new(&directives);
            let mut out = String::new();
            write!(out, "{}", instant.format_with_items(items))
                .map_err(|_| invalid("invalid strftime format"))?;
            Ok(out)
        },
    );
    env.render_str(&generation_blocks(template)?, context)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn generation_block_and_host_clock() {
        let template = "{% raw %}{% generation %}{% endraw %}{%- generation -%}{{ strftime_now('%Y/%m/%d %H%M') }}{%- endgeneration -%}";
        assert_eq!(
            render(
                template,
                Value::UNDEFINED,
                Some("2026-11-27T14:35:00+02:00".into())
            )
            .unwrap(),
            "{% generation %}2026/11/27 1435"
        );
    }

    #[test]
    fn python_methods_loops_and_json_format() {
        let ctx = Value::from(minijinja::value::Serde(
            serde_json::json!({"args":{"b":"<&","a":["ñ",false]}}),
        ));
        let template = "{% for k,v in args.items() %}{{ k }}{% break %}{% endfor %} {{ args|tojson }} {{ args|tojson(ensure_ascii=true, sort_keys=true, separators=(',', ':')) }}";
        assert_eq!(
            render(template, ctx, None).unwrap(),
            r#"b {"b": "<&", "a": ["ñ", false]} {"a":["\u00f1",false],"b":"<&"}"#
        );
    }
}
