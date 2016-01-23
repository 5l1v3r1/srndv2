//
// markup.go 
// memeposting markup parser
//
package srnd

import (
  "html"
  "regexp"
  "strings"
)

// copypasted from https://stackoverflow.com/questions/161738/what-is-the-best-regular-expression-to-check-if-a-string-is-a-valid-url
var re_external_link = regexp.MustCompile(`((?:(?:https?|ftp):\/\/)(?:\S+(?::\S*)?@)?(?:(?!(?:10|127)(?:\.\d{1,3}){3})(?!(?:169\.254|192\.168)(?:\.\d{1,3}){2})(?!172\.(?:1[6-9]|2\d|3[0-1])(?:\.\d{1,3}){2})(?:[1-9]\d?|1\d\d|2[01]\d|22[0-3])(?:\.(?:1?\d{1,2}|2[0-4]\d|25[0-5])){2}(?:\.(?:[1-9]\d?|1\d\d|2[0-4]\d|25[0-4]))|(?:(?:[a-z\u00a1-\uffff0-9]-*)*[a-z\u00a1-\uffff0-9]+)(?:\.(?:[a-z\u00a1-\uffff0-9]-*)*[a-z\u00a1-\uffff0-9]+)*(?:\.(?:[a-z\u00a1-\uffff]{2,}))\.?)(?::\d{2,5})?(?:[/?#]\S*)?)`);
var re_backlink = regexp.MustCompile(`>> ?([0-9a-f]+)`)

// parse backlink
func backlink(word string) (markup string) {
  re := regexp.MustCompile(`>> ?([0-9a-f]+)`)
  link := re.FindString(word)
  if len(link) > 2 {
    link = strings.Trim(link[2:], " ")
    if len(link) > 2 {
      url := template.findLink(link)
      if len(url) == 0 {
        return "<span class='memearrows'>&gt;&gt;" + link + "</span>"
      }
      // backlink exists
      return`<a href="`+url+`">&gt;&gt;` + link + "</a>"
    } else {
      return html.EscapeString(word)
    }
  }
  return html.EscapeString(word)
}
  
func formatline(line string) (markup string) {
  line = strings.Trim(line, "\t\r\n ")
  if len(line) > 0 {
    if strings.HasPrefix(line, ">") && ! ( strings.HasPrefix(line, ">>") && re_backlink.MatchString(strings.Split(line, " ")[0])) {
      // le ebin meme arrows
      markup += "<span class='memearrows'>"
      markup += html.EscapeString(line)
      markup += "</span>"
    } else if strings.HasPrefix(line, "==") && strings.HasSuffix(line, "==") {
      // redtext
      markup += "<span class='redtext'>"
      markup += html.EscapeString(line[2:len(line)-2])
      markup += "</span>"
    } else {
      // regular line
      // for each word
      for _, word := range strings.Split(line, " ") {
        // check for backlink
        if re_backlink.MatchString(word) {
          markup += backlink(word)
        } else {
          // linkify as needed
          word = html.EscapeString(word)
          markup += re_external_link.ReplaceAllString(word, `<a href="$1">$1</a>`)
        }
        markup += " "
      }
    }
  }
  markup += "<br />"
  return
}

// format lines inside a code tag
func formatcodeline(line string) (markup string) {
  markup += html.EscapeString(line)
  markup += "\n"
  return
}

func memeposting(src string) (markup string) {
  found_tag := false
  tag_content := ""
  tag := ""
  // for each line...
  for _, line := range strings.Split(src, "\n") {
    // beginning of code tag ?
    if strings.Count(line, "[code]") > 0 {
      // yes there's a code tag
      found_tag = true
      tag = "code"
    } else if strings.Count(line, "[spoiler]") > 0 {
      // spoiler tag
      found_tag = true
      tag = "spoiler"
    } else if strings.Count(line, "[psy]") > 0 {
      // psy tag
      found_tag = true
      tag = "psy"
    }
    if found_tag {
      // collect content of tag
      tag_content += line + "\n"
      // end of our tag ?
      if strings.Count(line, "[/"+tag+"]") == 1 {
        // yah
        found_tag = false
        var tag_open, tag_close string
        if tag == "code" {
          tag_open = "<pre>"
          tag_close = "</pre>"
        } else if tag == "spoiler" {
          tag_open = "<span class='spoiler'>"
          tag_close = "</span>"
        } else if tag == "psy" {
          tag_open = "<span class='psy'>"
          tag_close = "</span>"          
        }
        markup += tag_open
        // remove open tag, only once so we can have a code tag verbatum inside
        tag_content = strings.Replace(tag_content, "["+tag+"]", "", 1)
        // remove all close tags, should only have 1
        tag_content = strings.Replace(tag_content, "[/"+tag+"]", "", -1)
        // make into lines
        for _, tag_line := range strings.Split(tag_content, "\n") {
          if tag == "code" {
            markup += formatcodeline(tag_line)
          } else {
            markup += formatline(tag_line)       
          }
        }
        // close pre tag
        markup += tag_close
        // reset content buffer
        tag_content = ""
      }
      // next line
      continue
    }
    // format line regularlly
    markup += formatline(line)
  }
  // flush the rest of an incomplete code tag
  for _, line := range strings.Split(tag_content, "\n") {
    markup += formatline(line)
  }
  return 
}
