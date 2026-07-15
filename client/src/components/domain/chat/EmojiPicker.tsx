const EMOJIS = [
  "😀","😃","😄","😁","😆","😂","🤣","😊","🙂","😉","😍","😘","😜","🤔","🙄","😎","🤩","🥳","😢","😭",
  "😡","🤬","😱","🙏","👍","👎","👏","🙌","💪","🤝","✌️","👌","✋","🤚","🫶","❤️","🧡","💛","💚","💙",
  "💜","🖤","🤍","💔","💯","🔥","✨","🎉","🎊","🎁","📞","📱","💬","📩","📨","📷","📹","🎵","🎶","🔊",
];

export const EmojiPicker = ({ onPick }: { onPick: (e: string) => void }) => (
  <div className="grid w-64 grid-cols-8 gap-1 rounded-lg border bg-popover p-2 shadow-md">
    {EMOJIS.map((e) => (
      <button
        key={e}
        type="button"
        onClick={() => onPick(e)}
        className="h-7 w-7 rounded text-base hover:bg-muted"
      >
        {e}
      </button>
    ))}
  </div>
);