import { LRLanguage, LanguageSupport } from "@codemirror/language";
import { parser } from "lezer-beancount";

const beancountLanguage = LRLanguage.define({ parser });

export const beancount = () => new LanguageSupport(beancountLanguage);
