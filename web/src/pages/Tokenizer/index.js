import React, {useMemo, useState} from "react";
import cl100k_base from "gpt-tokenizer/encoding/cl100k_base";
import p50k_base from "gpt-tokenizer/encoding/p50k_base";
import r50k_base from "gpt-tokenizer/encoding/r50k_base";
import p50k_edit from "gpt-tokenizer/encoding/p50k_edit";
import "./style.css";
import {Button, Header, Label, Segment, Select, TextArea} from "semantic-ui-react";
// import NPMLogo from './NPMLogo.png'; // replace with the path to the NPM logo image

const tokenizers = {
    cl100k_base,
    p50k_base,
    r50k_base,
    p50k_edit
};

const pastelColors = [
    "rgba(107,64,216,.3)",
    "rgba(104,222,122,.4)",
    "rgba(244,172,54,.4)",
    "rgba(239,65,70,.4)",
    "rgba(39,181,234,.4)"
];

const monospace = `"Roboto Mono",sfmono-regular,consolas,liberation mono,menlo,courier,monospace`;

const TextInput = ({value, onChange}) => (
    <div className="ui form">
        <TextArea
            value={value}
            onChange={onChange}
            style={{fontFamily: monospace, width: "100%", minHeight: "200px", margin: "8px 0px"}}
        />
    </div>
);

const TokenizedText = ({tokens}) => (
    <div
        style={{
            display: "flex",
            flexWrap: "wrap",
            fontFamily: monospace,
            width: "100%",
            height: "200px",
            overflowY: "auto",
            padding: "8px",
            marginBottom: "8px",
            border: "1px solid #ccc",
            backgroundColor: "#f8f8f8",
            lineHeight: "1.5",
            alignContent: "flex-start"
        }}
    >
        {tokens.map((token, index) => (
            <span
                key={index}
                style={{
                    backgroundColor: pastelColors[index % pastelColors.length],
                    padding: "0 0px",
                    borderRadius: "3px",
                    marginRight: "0px",
                    marginBottom: "4px",
                    display: "inline-block",
                    height: "1.5em"
                }}
            >
        {
            <pre>
            {String(token)
                .replaceAll(" ", "\u00A0")
                .replaceAll("\n", "<newline>")}
          </pre>
        }
      </span>
        ))}
    </div>
);

const Tokenizer = () => {
    const [inputText, setInputText] = useState(
        "欢迎使用 GPT Token 计数器，替换你所需要计算的字符"
    );
    const [displayTokens, setDisplayTokens] = useState(false);

    const [selectedEncoding, setSelectedEncoding] = useState("cl100k_base");

    const api = tokenizers[selectedEncoding];
    const encodedTokens = api.encode(inputText);

    const decodedTokens = useMemo(() => {
        const tokens = [];
        for (const token of api.decodeGenerator(encodedTokens)) {
            tokens.push(token);
        }
        return tokens;
    }, [encodedTokens, api]);

    const toggleDisplay = () => {
        setDisplayTokens(!displayTokens);
    };

    const selectEncoding = (
        <div>
            <Label htmlFor="encoding-select">编码</Label>&nbsp;
            <Select
                id="encoding-select"
                value={selectedEncoding}
                onChange={(e, {name, value}) => {
                    setSelectedEncoding(value);
                }}
                options={[{
                    value: 'cl100k_base',
                    key: 'cl100k',
                    text: 'cl100k_base (3.5-turbo/4)'
                }, {
                    value: 'p50k_base',
                    key: 'p50k',
                    text: 'p50k_base'
                }, {
                    value: 'r50k_base',
                    key: 'r50k',
                    text: 'r50k_base'
                }, {
                    value: 'p50k_edit',
                    key: 'p50k_edit',
                    text: 'p50k_edit'
                }]}>
            </Select>
        </div>
    );

    return (
        <>
            <Segment>
                <Header as='h3'>计数器</Header>
                最全面的 GPT Token 计数器，支持 GPT-4
                <br/>
                <br/>
                <div className="tokenizer-container">
                    {selectEncoding}
                    <div className="tokenizer">
                        <TextInput
                            value={inputText}
                            onChange={(e) => setInputText(e.target.value)}
                        />
                        <Button onClick={() => setInputText("")}>清空</Button>
                    </div>

                    <TokenizedText tokens={displayTokens ? encodedTokens : decodedTokens}/>

                    <Button onClick={toggleDisplay}>
                        {displayTokens ? "显示分词文本" : "显示 Token ID"}
                    </Button>

                    <div className="statistics">
                        <div>字符: <strong>{inputText.length}</strong></div>
                        <div>Tokens: <strong>{encodedTokens.length}</strong></div>
                    </div>
                </div>
            </Segment>
        </>
    );
};

export default Tokenizer;