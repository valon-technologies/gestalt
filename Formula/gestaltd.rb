# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.43"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-macos-arm64.tar.gz"
      sha256 "d1dd13419ccbbafa7186e4e437cd371896007cdaf1b857c3e7c2ae59ae0577ff"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-macos-x86_64.tar.gz"
      sha256 "b10143a6950d4bb0ed70886919c366e63d9001efaa04b663b88dc03b49c55fa2"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-linux-arm64.tar.gz"
      sha256 "10372bb04408c5097ccb7c85df5220fa477c74a89587a57983392698cfab7332"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-linux-x86_64.tar.gz"
      sha256 "23dfac863e83228f6996fff768438a49b56fd6e81ea7e0fb9129b4a757ab2b56"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
