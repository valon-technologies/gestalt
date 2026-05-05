# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.14"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.14/gestaltd-macos-arm64.tar.gz"
      sha256 "bbe0e47713f761d476cd567028dcaae24ad2c2577dbaae09d485a9029ba8f678"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.14/gestaltd-macos-x86_64.tar.gz"
      sha256 "2671509712e152dcf8fdab97a9ebcff4724a888e190848c56906059fc745e739"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.14/gestaltd-linux-arm64.tar.gz"
      sha256 "ddea6e43e0532992c0a94e0b274ea97a8765f2f7db9d18cd8de96750da94d970"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.14/gestaltd-linux-x86_64.tar.gz"
      sha256 "f449409877d63b670c07df585361f8efabb976f79b9bff43a985f9353b89951c"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
