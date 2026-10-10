// Removes the comments from the CSS and JavaScript inlined into the pages.
//
// The sources are commented for whoever maintains them; none of that should be readable
// by whoever views the source of the published page. This is not a minifier: names,
// layout and line breaks are kept, only comments and the blank lines they leave go.
//
// Written as a scanner rather than a pair of regular expressions because a comment
// marker inside a string, a template literal or a regex literal is not a comment
// ('https://…', /[^/]*/). No dependency: the build has to run with plain node.
import vm from 'node:vm';

// A `/` opens a regex literal, rather than dividing, when what precedes it cannot end
// an expression: nothing at all, an operator or opening bracket, or one of these words.
const REGEX_AFTER_WORD = /(?:^|[^\w$.])(?:return|typeof|instanceof|in|of|case|do|else|void|delete|throw|new)$/;
const ENDS_EXPRESSION = /[\w$)\]]$/;

function tidy(code) {
  return code.replace(/[ \t]+\n/g, '\n').replace(/\n{2,}/g, '\n');
}

// One pass over the source. Literals are copied through verbatim and code is tidied
// around them, so the whitespace inside a string is never touched.
function scan(source, { js }) {
  const out = [];
  let code = '';
  let i = 0;

  const flushCode = () => { out.push(tidy(code)); code = ''; };
  const copyLiteral = (end) => { flushCode(); out.push(source.slice(i, end)); i = end; };

  // Index just past the closing delimiter of a quoted literal starting at `i`.
  const quotedEnd = (quote) => {
    let j = i + 1;
    while (j < source.length && source[j] !== quote) j += source[j] === '\\' ? 2 : 1;
    return j + 1;
  };

  // Index just past a regex literal starting at `i`, flags included. A `/` inside a
  // character class does not close it.
  const regexEnd = () => {
    let j = i + 1;
    let inClass = false;
    while (j < source.length && (inClass || source[j] !== '/')) {
      if (source[j] === '\\') j++;
      else if (source[j] === '[') inClass = true;
      else if (source[j] === ']') inClass = false;
      j++;
    }
    j++;
    while (/[a-z]/i.test(source[j] || '')) j++;
    return j;
  };

  const opensRegex = () => {
    const before = code.trimEnd();
    // Nothing since the last literal: a `/` right after a string divides.
    if (!before) return out.every((piece) => !piece);
    return !ENDS_EXPRESSION.test(before) || REGEX_AFTER_WORD.test(before);
  };

  while (i < source.length) {
    const c = source[i];
    const next = source[i + 1];
    if (c === '"' || c === "'" || (js && c === '`')) {
      copyLiteral(quotedEnd(c));
    } else if (c === '/' && next === '*') {
      const end = source.indexOf('*/', i + 2);
      if (end < 0) throw new Error('[build] unterminated /* comment');
      i = end + 2;
      code += ' '; // keeps the tokens on either side apart
    } else if (js && c === '/' && next === '/') {
      const end = source.indexOf('\n', i);
      i = end < 0 ? source.length : end;
    } else if (js && c === '/' && opensRegex()) {
      copyLiteral(regexEnd());
    } else {
      code += c;
      i++;
    }
  }
  flushCode();
  return out.join('').trim();
}

export function stripCssComments(css) {
  return scan(css, { js: false });
}

// The result is compiled once (never run): a mis-scanned literal would most likely
// leave the script unparseable, and that must fail the build, not ship.
export function stripJsComments(js) {
  const stripped = scan(js, { js: true });
  new vm.Script(stripped, { filename: 'inlined-script.js' });
  return stripped;
}
